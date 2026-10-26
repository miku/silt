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

## D2 Embedding backend — pending; facts gathered (2026-10-25)

The spike itself (hugot pure-Go, then a hand-written forward pass) has not run
yet. What fastembed 7.5.0 does for `AllMiniLML6V2`, read from
`~/.cargo/registry/src/*/fastembed-7.5.0`, which the Go side must reproduce:

- **Model:** HF repo `Qdrant/all-MiniLM-L6-v2-onnx` (patent's cache has
  snapshot `5f1b8cd`), file `model.onnx`, fp32, 384 dimensions. The cache is
  87 MB on disk, tokenizer included.
- **Tokenizer:** `tokenizer.json`, a BERT WordPiece tokenizer
  (`do_lower_case`, `tokenize_chinese_chars`, `strip_accents: null`).
- **Truncation:** `min(512, model_max_length = 512)` = 512 tokens. fastembed
  overrides the tokenizer config's own `max_length: 128`.
- **Padding:** `BatchLongest` with `[PAD]` (id 0); batches of 256. Mean
  pooling over the attention mask makes the padding irrelevant, up to float
  noise.
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
