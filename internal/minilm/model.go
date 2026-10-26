// Package minilm runs the all-MiniLM-L6-v2 sentence embedding model in pure
// Go, reproducing what fastembed's AllMiniLML6V2 computes: the BERT
// WordPiece tokenizer, a 6-layer BERT encoder, mean pooling over the tokens
// and L2 normalisation.
//
// It reads the same files fastembed downloads from the Hugging Face repo
// Qdrant/all-MiniLM-L6-v2-onnx: model.onnx for the weights and
// tokenizer.json for the tokenizer.
package minilm

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// The architecture of all-MiniLM-L6-v2 (its config.json).
const (
	hidden       = 384
	intermediate = 1536
	layers       = 6
	heads        = 12
	headDim      = hidden / heads
	vocabSize    = 30522
	maxPositions = 512
	typeVocab    = 2
	layerNormEps = 1e-12
)

// Dim is the length of an embedding.
const Dim = hidden

// Model is a loaded all-MiniLM-L6-v2. It is safe for concurrent use.
type Model struct {
	tok      *Tokenizer
	wordEmb  []float32 // [vocabSize × hidden]
	posEmb   []float32 // [maxPositions × hidden]
	typeEmb  []float32 // [typeVocab × hidden]; every token has type 0
	embNorm  layerNorm
	encoders [layers]encoderLayer
}

type encoderLayer struct {
	query, key, value, attnOut linearLayer
	attnNorm                   layerNorm
	inter, out                 linearLayer
	outNorm                    layerNorm
}

// linearLayer computes x·w + b for w of shape [in × out].
type linearLayer struct {
	w       []float32
	b       []float32
	in, out int
}

type layerNorm struct{ w, b []float32 }

// Load reads model.onnx and tokenizer.json from dir.
func Load(dir string) (*Model, error) {
	tj, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	tok, err := NewTokenizer(tj)
	if err != nil {
		return nil, err
	}
	onnx, err := os.ReadFile(filepath.Join(dir, "model.onnx"))
	if err != nil {
		return nil, err
	}
	tensors, matmuls, err := readONNX(onnx)
	if err != nil {
		return nil, err
	}
	w := &weights{tensors: tensors, matmuls: matmuls}
	m := &Model{
		tok:     tok,
		wordEmb: w.tensor("embeddings.word_embeddings.weight", vocabSize, hidden),
		posEmb:  w.tensor("embeddings.position_embeddings.weight", maxPositions, hidden),
		typeEmb: w.tensor("embeddings.token_type_embeddings.weight", typeVocab, hidden),
		embNorm: w.layerNorm("embeddings.LayerNorm"),
	}
	for l := range m.encoders {
		node := fmt.Sprintf("/encoder/layer.%d/", l)
		param := fmt.Sprintf("encoder.layer.%d.", l)
		m.encoders[l] = encoderLayer{
			query:    w.linear(node+"attention/self/query", param+"attention.self.query.bias", hidden, hidden),
			key:      w.linear(node+"attention/self/key", param+"attention.self.key.bias", hidden, hidden),
			value:    w.linear(node+"attention/self/value", param+"attention.self.value.bias", hidden, hidden),
			attnOut:  w.linear(node+"attention/output/dense", param+"attention.output.dense.bias", hidden, hidden),
			attnNorm: w.layerNorm(param + "attention.output.LayerNorm"),
			inter:    w.linear(node+"intermediate/dense", param+"intermediate.dense.bias", hidden, intermediate),
			out:      w.linear(node+"output/dense", param+"output.dense.bias", intermediate, hidden),
			outNorm:  w.layerNorm(param + "output.LayerNorm"),
		}
	}
	if w.err != nil {
		return nil, w.err
	}
	if len(tok.vocab) != vocabSize {
		return nil, fmt.Errorf("tokenizer.json: %d tokens, want %d", len(tok.vocab), vocabSize)
	}
	return m, nil
}

// Embed returns the normalised embedding of each text, computed
// concurrently.
func (m *Model) Embed(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(texts)) {
		wg.Go(func() {
			for i := range next {
				out[i] = m.EmbedOne(texts[i])
			}
		})
	}
	for i := range texts {
		next <- i
	}
	close(next)
	wg.Wait()
	return out
}

// EmbedOne returns the normalised embedding of text.
//
// fastembed pads a batch to its longest text and masks the padding; running
// each text at its own length computes the same thing without the padding.
func (m *Model) EmbedOne(text string) []float32 {
	ids := m.tok.Encode(text)
	n := len(ids)
	// The matrix products run on rows, a multiple of 4. The padding rows stay
	// zero throughout: no bias is added to them, GELU(0) is 0, and layer
	// normalisation, attention and pooling only look at the n real rows.
	rows := (n + 3) &^ 3
	x := make([]float32, rows*hidden)
	for t, id := range ids {
		row := x[t*hidden : (t+1)*hidden]
		word := m.wordEmb[int(id)*hidden:]
		pos := m.posEmb[t*hidden:]
		for j := range row {
			row[j] = word[j] + m.typeEmb[j] + pos[j]
		}
	}
	m.embNorm.apply(x, n)

	s := newScratch(rows)
	for l := range m.encoders {
		m.encoders[l].apply(x, n, s)
	}

	// Mean pooling over every token (the attention mask is all ones), then
	// normalisation as fastembed does it: v / (‖v‖ + 1e-12).
	emb := make([]float32, hidden)
	for t := range n {
		for j, v := range x[t*hidden : (t+1)*hidden] {
			emb[j] += v
		}
	}
	var sq float32
	for j := range emb {
		emb[j] /= float32(n)
		sq += emb[j] * emb[j]
	}
	norm := float32(math.Sqrt(float64(sq))) + 1e-12
	for j := range emb {
		emb[j] /= norm
	}
	return emb
}

// scratch holds the intermediate activations of one sequence, with room for
// its padding rows.
type scratch struct {
	rows              int
	q, k, v, ctx, tmp []float32 // [n × hidden]
	inter             []float32 // [n × intermediate]
	scores            []float32 // [n]
}

func newScratch(n int) *scratch {
	return &scratch{
		rows: n,
		q:    make([]float32, n*hidden), k: make([]float32, n*hidden), v: make([]float32, n*hidden),
		ctx: make([]float32, n*hidden), tmp: make([]float32, n*hidden),
		inter:  make([]float32, n*intermediate),
		scores: make([]float32, n),
	}
}

// apply runs one encoder layer on x, in place.
func (e *encoderLayer) apply(x []float32, n int, s *scratch) {
	e.query.apply(s.q, x, n, s.rows)
	e.key.apply(s.k, x, n, s.rows)
	e.value.apply(s.v, x, n, s.rows)
	attention(s.ctx, s.q, s.k, s.v, n, s.scores)
	e.attnOut.apply(s.tmp, s.ctx, n, s.rows)
	for i := range x {
		x[i] += s.tmp[i]
	}
	e.attnNorm.apply(x, n)

	e.inter.apply(s.inter, x, n, s.rows)
	for i, v := range s.inter[:n*intermediate] {
		s.inter[i] = gelu(v)
	}
	e.out.apply(s.tmp, s.inter, n, s.rows)
	for i := range x {
		x[i] += s.tmp[i]
	}
	e.outNorm.apply(x, n)
}

// attention is multi-head scaled dot-product self-attention over n tokens,
// writing the heads' outputs side by side into ctx.
func attention(ctx, q, k, v []float32, n int, scores []float32) {
	scale := float32(1 / math.Sqrt(headDim))
	clear(ctx)
	for h := range heads {
		off := h * headDim
		for i := range n {
			qi := q[i*hidden+off : i*hidden+off+headDim]
			maxScore := float32(math.Inf(-1))
			for j := range n {
				kj := k[j*hidden+off : j*hidden+off+headDim]
				var dot float32
				for d, qv := range qi {
					dot += qv * kj[d]
				}
				scores[j] = dot * scale
				maxScore = max(maxScore, scores[j])
			}
			var sum float32
			for j := range n {
				scores[j] = expf(scores[j] - maxScore)
				sum += scores[j]
			}
			ci := ctx[i*hidden+off : i*hidden+off+headDim]
			for j := range n {
				p := scores[j] / sum
				vj := v[j*hidden+off : j*hidden+off+headDim]
				for d := range ci {
					ci[d] += p * vj[d]
				}
			}
		}
	}
}

// apply normalises each of the n rows of x in place.
func (ln layerNorm) apply(x []float32, n int) {
	for t := range n {
		row := x[t*hidden : (t+1)*hidden]
		var mean float32
		for _, v := range row {
			mean += v
		}
		mean /= hidden
		var variance float32
		for _, v := range row {
			d := v - mean
			variance += d * d
		}
		variance /= hidden
		inv := float32(1 / math.Sqrt(float64(variance)+layerNormEps))
		for j, v := range row {
			row[j] = (v-mean)*inv*ln.w[j] + ln.b[j]
		}
	}
}

// apply computes dst = x·w + b for the n rows of x, multiplying all rows
// (rows ≥ n, padded) but adding the bias to the real ones only.
func (l linearLayer) apply(dst, x []float32, n, rows int) {
	matmul(dst, x, l.w, rows, l.in, l.out)
	for i := range n {
		row := dst[i*l.out : (i+1)*l.out]
		for j, b := range l.b {
			row[j] += b
		}
	}
}
