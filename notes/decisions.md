# silt — decisions

Short records of the M0 decisions in [todo.md](todo.md). Each says what was
decided and why; revisit by adding a dated note, not by rewriting.

## D1 Package layout — adopted (2026-10-25)

As proposed: library packages at the module root, the binary in `cmd/silt`.

```
silt (root)          doc.go, errors.go, version.go     ← src/lib.rs
cmd/silt/            flags, validation, resolution, output
model/  freshness/  sources/  rank/  verdict/  llm/…  tui/
internal/pipeline/   internal/config/   internal/ui/
internal/textutil/   Rust's char classes and lowercasing
scripts/paritytests/ generates notes/parity-tests.md
```

The root package holds only the error types and `Version()`, so the library
packages can import it without a cycle. `internal/textutil` was added during
M1: patent's tokenizing relies on `char::is_alphanumeric` and
`str::to_lowercase`, which draw different lines than Go's `unicode.IsLetter`
and `strings.ToLower` (Other_Alphabetic marks, circled letters, U+0130 and
the final sigma). Both are checked against `rustc` output.

Adapters are structs with exported `Client` and `BaseURL` fields plus a
`NewX(client)` constructor for the live host, the Go equivalent of patent's
`new` / `with_base_url`.

## D2 Embedding backend — hand-written pure Go + AVX2 kernel (2026-10-26)

**Decision:** candidate 2, a hand-written forward pass and WordPiece
tokenizer in `internal/minilm`, with one AVX2/FMA assembly kernel for the
matrix products. No cgo, so binaries stay static; no new dependencies apart
from `golang.org/x/text` (NFD) and `golang.org/x/sys/cpu`.

Measured against `testdata/embeddings/golden.json` (51 texts and 3 rankings,
recorded from patent's own `Ranker` by `scripts/patent-harness`), on an Intel
Core Ultra 7 258V (8 cores):

| | accuracy | 300 texts | load |
|---|---|---|---|
| patent (fastembed, ONNX Runtime) | — | 1.13–1.44 s | 0.21 s |
| hugot 0.8.3, pure-Go backend | 2 of 51 texts wrong, see below | 14.6–17.1 s | 0.06 s |
| silt, plain Go | pairwise cosines within 7.7e-7 | 5.4 s | 0.09 s |
| silt, AVX2/FMA kernel | pairwise cosines within 7.4e-7 | **0.86–0.92 s** | 0.09 s |

Accuracy, all texts: token ids identical to Hugging Face `tokenizers`;
1 − cos(silt, patent) ≤ 2.5e-12 per text; pairwise cosines within 7.4e-7 (the
bar was 1e-3); `rank_with` similarities within 4.4e-7. Embedding one idea
takes 14 ms.

- **hugot** has the arithmetic right (48 texts within 1e-5), but its Go
  tokenizer splits words at a zero-width space instead of dropping it
  (1 − cos = 0.45 for that text), and it does not truncate, so a text over
  512 tokens fails in the model. It is also 15× slower than patent and pulls
  in GoMLX and the onnxruntime bindings. Rejected.
- **silt never pads.** fastembed pads a batch to its longest text and masks
  the padding. Each text running at its own length computes the same thing,
  and it avoids a patent slowdown: with one 720-word description among 300
  matches, patent ranks for 17–25 s, because every text then costs 512
  tokens.
- **Numerics.** float32 throughout. GELU uses a float32 erf (Abramowitz and
  Stegun 7.1.26, error below 5e-7) over a Cephes-style `expf`; `math.Erf`
  took a third of the time. The matrix kernel computes 4×24 output tiles with
  12 accumulators; a 4×16 tile with 8 was 10% slower. Rows are padded to a
  multiple of 4 inside the forward pass, and the padding rows stay zero.
- **Weights** come from the same `model.onnx` fastembed loads. The PyTorch
  export is unoptimised, and each linear layer's MatMul node keeps its module
  path (`/encoder/layer.0/attention/self/query/MatMul`), so `onnx.go`, about
  200 lines of protobuf reading, maps the weights without an ONNX runtime. Every shape is
  checked on load.
- **arm64** (Apple Silicon included) has a NEON kernel (4×16 tiles, 16
  accumulators) since M5. It passes the golden tests under qemu, and CI runs
  them on native arm64 Linux and macOS; its speed there is not measured
  yet. Without it, arm64 would run the plain-Go path, 5× slower. Under `-race` the golden tests are
  skipped, because instrumented float loops run about 30× slower, so `make test`
  runs the suite with and without the race detector.

What fastembed 7.5.0 does for `AllMiniLML6V2`, read from
`~/.cargo/registry/src/*/fastembed-7.5.0`, which the Go side reproduces:

- **Model:** HF repo `Qdrant/all-MiniLM-L6-v2-onnx` (patent's cache has
  snapshot `5f1b8cd`), file `model.onnx`, fp32, 384 dimensions. The cache is
  87 MB on disk, tokenizer included.
- **Tokenizer:** `tokenizer.json`, a BERT WordPiece tokenizer
  (`do_lower_case`, `tokenize_chinese_chars`, `strip_accents: null`). From
  the `tokenizers` 0.23 source: the normaliser drops NUL, U+FFFD and Cc/Cf/Co
  characters (so a zero-width space vanishes), maps whitespace to spaces,
  pads CJK ideographs with spaces, strips accents (NFD, drop Mn) and
  lowercases per character. The pre-tokeniser splits on whitespace and
  isolates ASCII and Unicode punctuation. WordPiece makes words over 100
  characters `[UNK]`. Added tokens such as `[CLS]` match in the raw text
  first.
- **Truncation:** `min(512, model_max_length = 512)` = 512 tokens. fastembed
  overrides the tokenizer config's own `max_length: 128`.
- **Padding:** `BatchLongest` with `[PAD]` (id 0); batches of 256. Mean
  pooling over the attention mask makes the padding irrelevant, up to float
  noise; silt doesn't pad at all.
- **Inputs:** `input_ids`, `attention_mask`, and `token_type_ids` (all zeros)
  if the graph declares them.
- **Pooling:** mean over the attention mask.
- **Normalisation:** `v / (‖v‖ + 1e-12)`.

Ranking then takes a plain cosine (`rank.rs:12`), so the normalisation has no
effect on scores beyond rounding.

## D3 CLI library — cobra + pflag (2026-10-25)

Adopted as proposed, with these details:

- Comma lists (`--sources`, `--exclude`) and `--limit` are custom
  `pflag.Value`s rather than pflag's `StringSlice` and `Uint32`. `StringSlice`
  parses CSV, so `--sources ""` gives an empty list (auto-detect), whereas clap
  gives `[""]` and fails with "unknown source ``". Splitting on `,` matches
  clap.
- Cobra's default `completion` subcommand is disabled, since it would shadow
  an idea that starts with that word. `--completions <shell>` generates bash,
  zsh, fish and powershell. clap also generates elvish, but cobra doesn't, so
  elvish is dropped (see the deviations table in todo.md).
- Env fallbacks (`SILT_MODEL`, `SILT_API_BASE`, `SILT_API_KEY`) are resolved
  by hand with `os.LookupEnv`, so a variable that is set but empty counts as
  set, as it does for clap's `env`. Optional settings are `*string`
  throughout, keeping patent's `Option<String>` distinction between unset and
  empty.
- Exit codes: argument errors exit 2, as clap's do; runtime errors exit 1
  with `Error: <message>` on stderr, as anyhow's `main` does.

## D4 TUI stack — bubbletea, provisionally (2026-10-25)

`charmbracelet/bubbletea` + `lipgloss` + `bubbles`, `atotto/clipboard`,
`pkg/browser`. Not exercised until M7; revisit then.

## D5 Smaller libraries (2026-10-25)

- HTML: `PuerkitoBio/goquery` (M4).
- TOML: `pelletier/go-toml/v2` with `DisallowUnknownFields`.
- TTY detection: `golang.org/x/term`.
- HTTP, concurrency, tests: stdlib (`net/http`, `httptest`, `sync`).
- **Tests use plain `testing`**, without testify: table tests plus
  `reflect.DeepEqual` cover what patent's asserts do, and the dependency list
  stays short.
- Lint: golangci-lint v2.15.0, run with `go run` so that it is built with the
  module's Go 1.27 toolchain. Older binaries can't load the export data.

## D6 Error model (2026-10-25)

`errors.go` mirrors patent's `Error` enum as typed errors: `HTTPError`,
`HTTPClientError`, `ParseError`, `UnavailableError`, `LLMUnreachableError`,
`LLMRejectedError` and `EmbeddingError`, with the same `Display` texts.
`IsRetryable(err)` is false for Unavailable and HTTPClient.

- **A body that fails to decode is an `HTTPError`**, not a `ParseError`,
  because reqwest's `.json()` failure is a `reqwest::Error` and so
  `Error::Http`. This matters in M6: `assess` retries on `Http`.
- **Required fields.** serde fails a response that lacks a non-`Option`
  field. encoding/json fills in a zero value instead, which would turn a
  registry's shape change into a silent "zero results". Adapters therefore
  decode required fields into pointers and fail with "missing field `x`"
  when one is nil, so drift reads as "not reached", never as "no prior art".
  The same goes for trailing garbage after the JSON value: bodies go through
  `json.Unmarshal`, not a streaming decoder.
- Building a `net/http` client cannot fail, so `HTTPClientError` is never
  returned today. It exists so the taxonomy and `IsRetryable` stay complete.
