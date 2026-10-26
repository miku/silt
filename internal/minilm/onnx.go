package minilm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

// onnxTensor is a float32 initializer of an ONNX graph.
type onnxTensor struct {
	dims []int
	data []float32
}

// readONNX reads the float32 initializers of an ONNX model, keyed by name,
// and for each MatMul node whose weight is an initializer, the node's name
// and that weight's name: the PyTorch exporter names the weights of linear
// layers anonymously ("onnx::MatMul_788") but keeps the module path in the
// node ("/encoder/layer.0/attention/self/query/MatMul").
//
// It decodes just the protobuf fields it needs (onnx.proto: ModelProto.graph
// = 7; GraphProto.node = 1, initializer = 5; NodeProto.input = 1, name = 3,
// op_type = 4; TensorProto.dims = 1, data_type = 2, name = 8, raw_data = 9).
func readONNX(b []byte) (map[string]onnxTensor, map[string]string, error) {
	tensors := make(map[string]onnxTensor)
	matmuls := make(map[string]string)
	err := fields(b, func(num int, v []byte, _ uint64) error {
		if num != 7 {
			return nil
		}
		return fields(v, func(num int, v []byte, _ uint64) error {
			switch num {
			case 1:
				var inputs []string
				var name, op string
				if err := fields(v, func(num int, v []byte, _ uint64) error {
					switch num {
					case 1:
						inputs = append(inputs, string(v))
					case 3:
						name = string(v)
					case 4:
						op = string(v)
					}
					return nil
				}); err != nil {
					return err
				}
				if op == "MatMul" && len(inputs) == 2 {
					matmuls[name] = inputs[1]
				}
			case 5:
				name, t, err := readTensor(v)
				if err != nil {
					return fmt.Errorf("initializer %s: %w", name, err)
				}
				if t.data != nil {
					tensors[name] = t
				}
			}
			return nil
		})
	})
	if err != nil {
		return nil, nil, fmt.Errorf("model.onnx: %w", err)
	}
	// Keep only the MatMuls with a weight; the attention ones multiply two
	// activations.
	maps.DeleteFunc(matmuls, func(_, w string) bool { _, ok := tensors[w]; return !ok })
	return tensors, matmuls, nil
}

func readTensor(b []byte) (string, onnxTensor, error) {
	var (
		name  string
		t     onnxTensor
		dtype uint64
		raw   []byte
	)
	err := fields(b, func(num int, v []byte, x uint64) error {
		switch num {
		case 1:
			if v == nil {
				t.dims = append(t.dims, int(x))
				return nil
			}
			for len(v) > 0 { // packed
				d, n := binary.Uvarint(v)
				if n <= 0 {
					return errBadProto
				}
				t.dims = append(t.dims, int(d))
				v = v[n:]
			}
		case 2:
			dtype = x
		case 8:
			name = string(v)
		case 9:
			raw = v
		}
		return nil
	})
	if err != nil || dtype != 1 || raw == nil { // 1 is FLOAT; skip the int64 shape constants
		return name, onnxTensor{}, err
	}
	n := 1
	for _, d := range t.dims {
		n *= d
	}
	if len(raw) != 4*n {
		return name, onnxTensor{}, fmt.Errorf("%d bytes of data for shape %v", len(raw), t.dims)
	}
	t.data = make([]float32, n)
	for i := range t.data {
		t.data[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return name, t, nil
}

var errBadProto = errors.New("malformed protobuf")

// fields calls f with each field of the protobuf message b: v holds the bytes
// of a length-delimited field, x the value of a varint. Other wire types are
// skipped.
func fields(b []byte, f func(num int, v []byte, x uint64) error) error {
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return errBadProto
		}
		b = b[n:]
		num, typ := int(tag>>3), tag&7
		switch typ {
		case 0: // varint
			x, n := binary.Uvarint(b)
			if n <= 0 {
				return errBadProto
			}
			b = b[n:]
			if err := f(num, nil, x); err != nil {
				return err
			}
		case 1: // 64-bit
			if len(b) < 8 {
				return errBadProto
			}
			b = b[8:]
		case 2: // length-delimited
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return errBadProto
			}
			v := b[n : n+int(l)]
			b = b[n+int(l):]
			if err := f(num, v, 0); err != nil {
				return err
			}
		case 5: // 32-bit
			if len(b) < 4 {
				return errBadProto
			}
			b = b[4:]
		default:
			return errBadProto
		}
	}
	return nil
}

// weights looks up the model's parameters by name and checks their shapes.
type weights struct {
	tensors map[string]onnxTensor
	matmuls map[string]string
	err     error
}

func (w *weights) tensor(name string, dims ...int) []float32 {
	t, ok := w.tensors[name]
	if !ok {
		w.fail(fmt.Errorf("no tensor %s", name))
		return nil
	}
	if !slices.Equal(t.dims, dims) {
		w.fail(fmt.Errorf("tensor %s has shape %v, want %v", name, t.dims, dims))
		return nil
	}
	return t.data
}

// linear is the layer whose MatMul node is named node and whose bias is
// bias.
func (w *weights) linear(node, bias string, in, out int) linearLayer {
	name, ok := w.matmuls[node+"/MatMul"]
	if !ok {
		w.fail(fmt.Errorf("no MatMul node %s", node))
	}
	return linearLayer{w: w.tensor(name, in, out), b: w.tensor(bias, out), in: in, out: out}
}

func (w *weights) layerNorm(prefix string) layerNorm {
	return layerNorm{w: w.tensor(prefix+".weight", hidden), b: w.tensor(prefix+".bias", hidden)}
}

func (w *weights) fail(err error) {
	if w.err == nil {
		w.err = fmt.Errorf("model.onnx: %w", err)
	}
}
