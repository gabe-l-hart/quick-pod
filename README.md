# podcast — research-driven podcast generator

A simple Go binary that turns a topic into a 15–20 minute podcast:

```
topic ──► research agent ──► script writer ──► critic ──► decision
              │                                   │
   arXiv / web search / docs / trusted URLs        │
              └────────────── loop until PASS ─────┘
                            │
                    ┌───────▼────────┐
                    │  TTS → audio.wav  │
                    └────────────────┘
```

The research stage is tool-calling and grounded in the configured sources
(general search, arXiv, trusted URLs, and any user-supplied documents). The
writer and the critic are **separate models** — the critic is an
**independently-configured LLM**, plus an optional **System One decision
endpoint** (TypeSafe Jev / llama.cpp `/v1/systemone`) for the pass/fail verdict.

## Build

```
go build -o podcast ./cmd/podcast
```

No external services are required to build or test. Tests run offline
(`httptest` mocks + golden files), and there is an offline dev mode:

```
./podcast --topic "..." --mock      # deterministic mock LLM, no network
```

## Quick start

```
# 1. copy and fill in the example config
cp examples/podcasts.yaml podcasts.yaml
#    (edit llm/critic base_url, model, keys; see examples/podcasts.yaml)

# 2. export env-var keys or edit the file
export LLM_API_KEY=...

# 3. run
./podcast --topic "retrieval-augmented generation" \
  --docs ../papers/attention.pdf \
  --docs 1706.03762

# 4. stop before audio (no TTS spend)
./podcast --topic "..." --dry-run

# 5. offline dev run (no config needed)
./podcast --topic "..." --mock
```

## How it works

1. **Topic** — `--topic` (required) or interactive prompt.
2. **Documents** — `--docs` (repeatable): local `.pdf/.txt/.md/.html`, URLs,
   and arXiv IDs (`1706.03762`, `https://arxiv.org/abs/1706.03762`).
   Converted to normalized text; failures are logged and skipped.
3. **Research** — a bounded tool-calling loop (max ~8 steps): `web_search`,
   `arxiv_search` (keyless), `fetch_url` (trusted URLs only), `read_document`.
   Produces a cited Markdown brief.
4. **Summarize** — the brief is condensed for the writer.
5. **Write** — a structured JSON script (title, segments, word budget for the
   configured minutes/wpm).
6. **Critique** — the independent critic scores fit/accuracy/coverage/length/
   engagement via **constrained decoding** (OpenAI `response_format` or llama.cpp
   top-level `json_schema`; optional raw GBNF grammar).
7. **Decision** — `decision: "llm"` (default): the pass/fail verdict comes from
   the constrained critique. Or `decision: "systemone"`: the critic produces the
   full evaluation, then the verdict comes from a TypeSafe Jev / llama.cpp
   `/v1/systemone` call (`noul` probability ≥ threshold).
8. **Loop** — steps 4–7 repeat until the verdict is PASS or the iteration cap
   is hit; issues are fed back into the next write.
9. **Render** — script segments are chunked (≤4096 chars) and synthesized via
   `/v1/audio/speech` (cloud or local llama.cpp); the chunks are concatenated
   into one WAV.

Output (per episode, `<out-dir>/<slug>/`): `script.md`, `research.md`,
`critique.json`, `meta.json` (config snapshot, sources, failures),
`iterations/iteration-N.{md,json}`, and `audio.wav` if rendered
(after a PASS verdict, or with `--force-render`).

## Configuration

One file (`podcasts.yaml`, or `--config`). All prompts are centralized in
`internal/prompts/prompts.go` and can be overridden via the `prompts:` section
(priority: inline `overrides` > `files` > baked-in default). See
`examples/podcasts.yaml` for a fully-commented template and
`examples/prompts_example_note.md` for prompt details.

Config is validated at startup (missing keys, bad ranges, unreadable files).

## Local llama.cpp

Tested design target — run three servers, then point `podcasts.yaml` at them:

```
llama-server -hf ggml-org/Qwen3-TTS-12Hz-1.7B-Base-GGUF -m model.gguf -c 8192   # TTS (/v1/audio/speech)
llama-server -hf ggml-org/OpenJev-GGUF -m model.gguf                              # decision model (/v1/systemone)
llama-server -hf ggml-org/my-model-GGUF -m model.gguf                              # primary + critic LLM (/v1/chat/completions)
```

`/v1/audio/speech` is OpenAI-compatible — the same client works for cloud and
local. Local TTS options (see PLAN.md §6.6): Pocket TTS (lightest, best on-device),
Kokoro-82M (natural, cheap CPU), Qwen3-TTS (highest quality, GPU-friendly),
Chatterbox. Do not run TTS on Termux/Android — use a LAN machine.

## Architecture

```
cmd/podcast/main.go          entry point
internal/
  cli/                       orchestration (flags → pipeline → render → artifacts)
  config/                    one config file, env expansion, validation
  prompts/                   centralized, configurable prompts (baked-in defaults)
  llm/                       OpenAI-compatible chat client (retries, tool calling, constrained decoding)
  documents/                 pdf/html/arXiv/text converters (pure-Go, no cgo)
  search/                    arXiv (keyless) + Brave/Tavily/Serper
  research/                  tool-calling agent → cited brief
  pipeline/                  script writer, critic, iteration loop
  decision/                  System One decision model (Jev / llama.cpp)
  tts/                       /v1/audio/speech client + chunking
  audio/                     WAV parse/concat
  artifacts/                 script/research/critique/meta writers
  mock/                      deterministic offline LLM (dev mode)
```

## Testing

```
go test ./...
```

All tests run offline. Golden fixtures (`testdata/fixture.pdf`), mock HTTP
servers for `llm`/`search`/`decision`, a mock LLM through the whole loop.

## Status / known limits

- MP3 assembly needs ffmpeg (WAV is the default, fully supported).
- The systemone adapter implements the confirmed `/v1/systemone` contract
  (`state` + `questions:{...}` → `answers:{...}`) — verified against the
  llama.cpp source; a live end-to-end test requires a decision model server.
- One narrator voice; the data model does not preclude host+guest later.
- Search provider contracts for Brave/Tavily/Serper are best-effort and should
  be re-checked against the current provider docs before enabling.
- Config gaps (real keys, endpoints, models, TTS voice) are intentionally
  left for the user — see `examples/podcasts.yaml`.
