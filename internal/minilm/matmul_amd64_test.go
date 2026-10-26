package minilm

import "testing"

// withoutAVX2 runs the plain-Go matmul, as on arm64 or an older CPU.
func withoutAVX2(t testing.TB) {
	orig := hasKernel
	hasKernel = false
	t.Cleanup(func() { hasKernel = orig })
}

func TestEmbeddingsMatchPatentInPlainGo(t *testing.T) {
	withoutAVX2(t)
	checkGoldenEmbeddings(t)
}

func BenchmarkEmbed300PlainGo(b *testing.B) {
	withoutAVX2(b)
	m := loadModel(b)
	texts := benchTexts(b, 300)
	for b.Loop() {
		m.Embed(texts)
	}
}
