# todo

## embedding options

Currently we use https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2

> This is a sentence-transformers model: It maps sentences & paragraphs to a
> 384 dimensional dense vector space and can be used for tasks like clustering
> or semantic search.

Which is great, because it works and is really small (compared to later
models); it also saw 182,461,837 downloads on HF in the last month as of
2026-11-03. It has 22.7M parameters and a fast Go/ASM inference can be written
in less than 2K lines of code, cf. @../internal/minilm.

A modern option could be https://huggingface.co/google/embeddinggemma-2

> EmbeddingGemma 2 is an open multimodal embedding model built by Google
> DeepMind which maps text (incl. code), images, video, and audio inputs—and
> combinations thereof—into a single, unified 768-dimensional vector space. The
> model has 740M total parameters, combining a 270M parameter text model with
> modular vision (170M) and audio (300M) encoders.

That is about 33x the size of "minilm", and would take likely some care to make
it small and fast.
