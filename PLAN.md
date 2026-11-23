# Podcast Researcher — Build Plan

**Status:** Plan only — not yet approved for implementation.
**Target:** A simple Go binary (`podcast`) that takes a topic, conducts web/document
research with an LLM, writes a 15–20 minute podcast script, critiques and revises it,
and renders the final audio with a configured TTS model.

---

## 1. Goal & scope

### What it does
1. User provides a **topic description** and optionally **source documents / links**.
2. A **research agent** gathers context from configured sources: general web search,
   arXiv (keyless), and user-defined trusted sources.
3. An **LLM** writes a podcast script (15–20 min, ~2,250–3,000 words at ~150 wpm).
4. A **critic** evaluates the script against the topic (fit, accuracy, coverage,
   length, engagement) and returns scored issues.
5. Steps 3–4 repeat until the script passes or a max iteration count is hit.
6. The script is rendered to audio via a configured **TTS model**.

### Non-goals (for the MVP)
- Multi-voice dialogue / host + guest personas (single narrator voice; the data model
  will not preclude it later).
- A web UI or interactive TUI beyond a thin CLI.
- Streaming/real-time output, resume-from-failure, or a server mode.
- Republishing/hosting the episode (output is files on disk).

---

## 2. Tech choices

| Area | Choice | Rationale |
|---|---|---|
| Language | Go 1.27 (single static-ish binary, no cgo) | Per requirement |
| Config | YAML via `gopkg.in/yaml.v3`; hand-rolled loader | Tiny dep tree; typed, validated structs |
| HTTP | stdlib `net/http` | No deps |
| LLM | OpenAI-compatible `/v1/chat/completions` + tool calling + constrained decoding (stdlib) | One client covers OpenAI, Ollama, llama.cpp server, LM Studio, Together, etc. — **primary LLM = one section in config**; **critic = its own independent section** |
| arXiv | stdlib `net/http` + XML parsing (or `mmcdole/gofeed`) | Public, keyless API |
| PDF text | `github.com/ledongthuc/pdf` (pure Go, no deps) | Works in Termux / Android without cgo; extract plain text |
| TTS | HTTP client to an OpenAI-compatible `/v1/audio/speech` endpoint — cloud (OpenAI) **or local llama.cpp** (`llama-server`, libmtmd TTS build) | Same client for both; local server exposes `/v1/audio/speech` (OpenAI shape) and raw `/tts` |
| Audio | WAV synthesis (PCM), segment concat, pure Go; optional ffmpeg for MP3 encode | No external audio deps required |
| Testing | `httptest` mock LLM/search servers; golden tests | No real API calls in CI |

**Runtime targets:** POSIX + Termux/Android (arm64). Pure-Go dependency set so the
binary stays portable and needs no cgo toolchain.

---

## 3. Architecture

```
                ┌──────────────────────────────────────────────┐
                │                  CLI (main)                  │
                │  flags: --topic --docs --config --out       │
                └───────────────────────┬──────────────────────┘
                                        │
             ┌──────────────────────────┴─────────────────────────┐
             │                  Pipeline driver                   │
             │   1. load & validate config                        │
             │   2. convert documents (docs module)               │
             │   3. research loop { research → write → critique } │
             │   4. render audio (TTS + concat)                   │
             │   5. write artifacts                               │
             └───────┬──────────────────┬──────────────────┬──────┘
                     │                  │                  │
              ┌──────▼──────┐   ┌───────▼────────┐   ┌─────▼──────┐
              │ documents/  │   │ research/ +     │   │ tts/ +     │
              │ converters  │   │ agents/         │   │ audio/     │
              └─────────────┘   └───────┬────────┘   └────────────┘
                                        │
                                 ┌──────▼──────┐
                                 │ llm/ client │  ── OpenAI-compatible
                                 │ (1 impl)    │     /chat/completions
                                 └─────────────┘
```

The pipeline driver is deliberately **dumb**: it sequences calls and decides whether
to continue the loop. All intelligence lives in the agents. This keeps the loop
testable without real LLMs (mock client injected).

---

## 4. Config surface (the single config)

One file (`podcasts.yaml`, default searched in cwd → `$XDG_CONFIG_HOME/podcasts/`),
plus env var expansion. The **primary LLM section** drives research, summarization
and writing. The **critic section** is configured independently — it may point at
the same server as the primary — and drives the critique + decision in the
revision loop (§6.5).

```yaml
# podcasts.yaml — example (not committed; real keys never committed)

llm:                                  # PRIMARY LLM — research, summarization, writing
  base_url: "http://127.0.0.1:8080/v1"   # OpenAI-compatible endpoint
  api_key: "${LLM_API_KEY}"               # env var reference or literal (optional if server open)
  model: "my-model"
  temperature: 0.7
  max_tokens: 4096

critic:                               # INDEPENDENT critic — same file, separate section
  llm:                                # independent model for the detailed critique
    base_url: "http://127.0.0.1:8080/v1"   # often the same llama.cpp server
    api_key: "${CRITIC_API_KEY}"           # optional if the same server is open
    model: "critic-model"
    temperature: 0.2
    max_tokens: 2048
  decision: "llm"                    # "llm" | "systemone"
  systemone:                         # required only when decision == "systemone"
    base_url: "http://127.0.0.1:8080/v1"
    path: "/v1/systemone"              # jev-compatible decision endpoint
    api_key: "${SYSTEMONE_API_KEY}"     # optional if the server is open
  grammar: ""                        # optional: path to an explicit GBNF grammar (llama.cpp only)

tts:                                   # TTS — cloud or local llama.cpp (§6.6)
  base_url: "http://127.0.0.1:8083/v1"   # local: llama-server; cloud: e.g. https://api.openai.com/v1
  api_key: "${TTS_API_KEY}"               # optional locally
  model: "pocket-tts"                     # server-reported model name
  voice: "default"                        # model-dependent (built-in voice id / reference WAV)
  format: "wav"                           # mp3/wav/opus/aac supported by server
  speed: 1.0

search:
  provider: "brave"                       # brave | tavily | serper | none
  api_key: "${SEARCH_API_KEY}"            # required when provider != none
  max_results_per_query: 8

sources:
  arxiv: true                             # always keyless
  trusted_urls:                           # user-configured trusted sources
    - "https://arxiv.org/abs/"            # prefix match
    - "https://example.org/resources/"    # prefix match

pipeline:
  max_iterations: 4                       # research+write+critique loops
  target_minutes: 17                      # 15–20 min window
  words_per_minute: 150
  pass_threshold: 0.8                     # critique score needed to proceed ("llm" path)
  max_research_steps: 8                   # tool-call cap per research pass
  time_budget: "10m"                      # global wall-clock cap

paths:
  output_dir: "out"
  cache_dir: ".podcasts-cache"            # optional TTS/research cache directory
```

**Validation** (fail fast on startup): `llm.base_url` + `llm.model` required;
`critic.llm` likewise, and `critic.decision` must be `"llm"` or `"systemone"` — if
`"systemone"`, `critic.systemone.base_url` + `critic.systemone.path` are required.
`tts.base_url` + `tts.model` required. `search.provider != none` requires an API
key; at least one source must be enabled (`arxiv: true` or a search provider, or
trusted URLs). `target_minutes ∈ [15, 20]`, `max_iterations ≥ 1`,
`pass_threshold ∈ (0, 1]`. API keys are resolved from env vars and must be present
for non-keyless providers — fail fast if missing.

**Config surface = one file, one loader.** It has two LLM-ish sections (primary +
critic), same client implementation, different endpoint/model. Everything else
either tunes behavior or points at external services.

---

## 5. Runtime flow (detailed)

### Step 1 — Topic input
CLI: `podcast --topic "the impact of retrieval-augmented generation on scientific reviews"`
or interactive prompt if omitted. Input validated (non-empty, length cap).

### Step 2 — Source documents / links
`--docs` accepts: arXiv IDs (`1706.03762`), arXiv URLs (`https://arxiv.org/abs/1706.03762`),
local files (`.pdf`, `.txt`, `.md`, `.html`, `.epub`*), and remote URLs (`.pdf`, `.html`,
`.txt`, `.md`). Each is normalized by the **document conversion module** (§6.1) into
`Document{ Source, Kind, Title, Text, Metadata }`. Unreadable docs are recorded as
**failures** (logged, non-fatal) — the pipeline proceeds with what it has.

*`.epub` is stretch; MVP covers pdf/txt/md/html/arxiv.*

### Step 3 — Research agent
An LLM-driven tool-calling loop (see §6.2):

- Tools:
  - `web_search(query, n)` → general search provider (Brave/Tavily/Serper).
  - `arxiv_search(query, n)` → `export.arxiv.org/api/query` (keyless).
  - `fetch_url(url)` → fetch + convert a trusted source to text.
  - `read_document(id)` → read from the normalized corpus of user-supplied docs.
- Output: `ResearchBrief{ Topic, Findings[] }` — a Markdown brief with inline citations
  (`[n]` → URL/source), plus a `Sources[]` registry for later citation.
- Capped by `max_research_steps`; graceful degradation if a tool errors.

### Step 4 — Script writer
One LLM call (system + user prompt) producing a **structured script**:

```go
type Script struct {
    Title     string
    Summary   string          // for the episode card
    Segments  []Segment       // ordered
}
type Segment struct {
    Kind      string  // intro | body | synthesis | outro
    Title     string
    Text      string  // spoken text
    ApproxSec float64 // derived from word count / wpm
}
```

Target word budget computed from config (`target_minutes * wpm`); writer told exact
per-segment budgets. Prompt requires: accurate to the research brief + docs, engaging,
no fabricated facts, natural spoken style, no markdown formatting in spoken text.

### Step 5 — Critic / evaluator
One orchestration pass per iteration against the **independent critic** (§6.5).
Input: topic, research brief, script. Output (JSON schema):

```go
type Critique struct {
    Score        float64   // 0..1
    TopicFit     float64
    Accuracy     float64
    Coverage     float64
    Length       float64
    Engagement   float64
    Issues       []string  // concrete, actionable
    Pass         bool      // true iff Score >= threshold
}
```

### Step 5 (cont.) — Decision: two modes
The pass/fail decision for the loop is made one of two ways:

- **`decision: "systemone"`** — the critic produces the full structured critique
  (scores + issues, via constrained decoding); a second call posts
  `{ topic, critique, script_summary }` to the jev-compatible decision endpoint;
  its verdict is authoritative for the loop.
- **`decision: "llm"`** (default) — the decision is made by **prompting +
  constrained decoding**: the pass/fail verdict is itself a field of the
  constrained critique output (`pass = score >= pass_threshold`). No extra service.

### Steps 3–5 — Iteration loop
`research → write → critique → decision`, repeated until the verdict is a pass or
`max_iterations` is reached. On failure, the **critique issues are fed back** into
the next write prompt (and the research stage may re-run if coverage is weak).
Each iteration is logged and saved
(`out/<slug>/iterations/iteration-N.{md,json}`).

### Step 6 — TTS rendering
- Split script segments into TTS-sized chunks (≤ 4096 chars input limit).
- Call TTS per chunk (sequential or small bounded concurrency); cache by
  `(voice, model, text-hash)`.
- Assemble into a single `audio.wav`; optional ffmpeg encode to MP3 if available.
- Output artifacts:
  - `out/<slug>/script.md` — final script (markdown, human-readable)
  - `out/<slug>/audio.wav` (+ `.mp3` if ffmpeg)
  - `out/<slug>/research.md` — final research brief
  - `out/<slug>/critique.json` — final critique
  - `out/<slug>/meta.json` — config snapshot, iterations used, sources, timestamps

---

## 6. Component designs

### 6.1 Document conversion module (`internal/documents`)
**Interface:** `type Converter interface { CanHandle(doc Source) bool; Convert(ctx, src) (Document, error) }`

Converters (pluggable, order matters):
| Kind | Input | Conversion |
|---|---|---|
| Plain text / markdown | `.txt`, `.md` | read + normalize whitespace |
| HTML | `.html`, URLs ending in `.html` or pages | fetch + strip tags (simple HTML→text; no JS) |
| PDF | `.pdf` (local or URL) | `ledongthuc/pdf` → plain text |
| arXiv | URL `arxiv.org/abs/<id>`, raw ID, PDF URL | fetch PDF and convert (falls back to abstract page) |

Normalization rules: title extracted from metadata/first H1, text cleaned
(de-hyphenate lines, collapse whitespace), per-doc character budget for prompting
(truncation with `[... truncated ...]` marker + logged).

**Tests:** golden-file tests with real fixture PDFs/HTML/arXiv-style pages.

### 6.2 Research agent (`internal/research`)
- Tool definitions mirror the LLM's function-calling schema (stdlib JSON).
- Prompt: system prompt ("you are a research assistant..."), user prompt (topic +
  document corpus summary + sources), tools registered.
- Loop: `call LLM → if tool calls, execute (read-only) → feed results → repeat`
  until no tool calls or step cap. Max ~8 steps; ~1–3 searches expected.
- Results aggregated into `ResearchBrief` with a citation registry.
- Reuse the **primary LLM client** (the configured primary LLM) for research and
  writing — never the critic.

### 6.3 LLM client (`internal/llm`)
- `type Client struct{ baseURL, apiKey, model, temp, maxTokens, http *http.Client }`
- `Chat(ctx, []Message, []Tool) (Response, error)` — handles retries (exponential
  backoff, respects `Retry-After`), truncation (JSON parse, partial recovery).
- Single implementation covers chat + tool calling; streaming is **out of scope**.

### 6.4 Script writer (`internal/pipeline`)
- `WriteScript(ctx, topic, corpus, brief, issues []string) (Script, error)` — primary LLM.
- Prompt builders are pure functions → unit-testable snapshot tests.
- Structured output (json_schema) so the script parses reliably.

### 6.5 Critic & decision model (`internal/pipeline`, `internal/decision`)
The critic is a **separate client** (`critic.llm`) from the primary LLM, with
`temperature` and `max_tokens` tuned down. It produces the full evaluation as
structured JSON via **constrained decoding**, through either of:

- **Portable:** OpenAI-style `response_format: { type: "json_schema", ... }` in the
  chat call — supported by OpenAI and by the llama.cpp server (which compiles it to a
  GBNF grammar under the hood). This is the default and the only path needed for cloud.
- **llama.cpp-direct:** load an explicit GBNF grammar from a file
  (`critic.grammar`) and pass it as the `grammar` parameter — the strictest
  constraint, available on llama.cpp-server builds.

**Decision model — two modes:**

| Mode | When | What happens |
|---|---|---|
| `decision: "llm"` (default) | no systemone configured | The pass/fail decision IS a field of the constrained critique output (`pass = score >= pass_threshold`). Pure prompting + constrained decoding. |
| `decision: "systemone"` | `critic.systemone` configured | The critic produces the full critique (as above); then a **second** call posts `{ topic, critique, script_summary }` to the jev-compatible `/v1/systemone` endpoint; its verdict is authoritative for the loop. |

The systemone adapter lives in its own package (`internal/decision`) so the
(unspecified) request/response contract can be pinned without touching pipeline
code: `type DecisionModel interface { Decide(ctx, CriticInput) (Decision, error) }`,
with the systemone and the llm modes as the two implementations.

### 6.6 TTS + audio
- `internal/tts`: `Synthesize(ctx, text) (io.ReadCloser, error)`; segments chunked
  to ≤4096 chars; on error → retry w/ backoff.
- `internal/audio`: parse/append WAV PCM frames; optional MP3 via ffmpeg subprocess.

**Two sourcing paths, one client.** `tts.base_url` points at any
OpenAI-compatible `/v1/audio/speech` endpoint:

- **Cloud** — e.g. OpenAI (`https://api.openai.com/v1`) — API key required.
- **Local llama.cpp** — `llama-server` with a TTS-capable build (libmtmd) exposes
  BOTH `POST /tts` (raw WAV, `{input, speaker_reference?}`) and the
  **OpenAI-compatible `POST /v1/audio/speech`** (same endpoint and client as the
  cloud path — just set `base_url` at the local server and leave the key optional).

**Local TTS options** (llama.cpp-supported models, picked via `--model` at server start):

| Model | Size / Notes |
|---|---|
| Pocket TTS | lightest; ~100–200 MB; English + partial multilingual; voice clone via reference WAV; best CPU/on-device fit |
| Kokoro-82M | 82M params; 8 languages, 54 fixed voices; Apache-2.0; ~0.3–0.5× RTF on CPU; no cloning; widely deployed |
| Qwen3-TTS 1.7B | ~2.1 GiB (Q8_0); 10 languages; voice cloning; GPU-friendly; best quality via server |
| Chatterbox | high quality, harder on CPU |
| VoxCPM2 / LFM3-Audio / Soprano / tiny-voca / ZONOS3 / Fish Audio S2 Pro | other options; pick by quality/size trade-off |

**Alternatives outside llama.cpp** (not the default): Piper (ONNX, very fast on
1–2 CPU cores; external binary + `.onnx` voice pair; no cloning); Kokoro via
in-process ONNX Runtime (no llama.cpp needed; heavier dependency stack); cloud TTS
via `/v1/audio/speech` — zero local compute, needs internet + key.

**Recommendation for this project:** start with **Pocket TTS** or **Kokoro** on the
local llama.cpp server (or Qwen3-TTS if you have a GPU), run `llama-server` with
the TTS-capable build, point `tts.base_url` at it. On Termux/Android, do not run
TTS on-device; run `llama-server` on a LAN machine and point the binary at it —
the client and config are identical.

---

## 7. Data model highlights (Go)

```go
type Document struct {
    Source    string // original input
    Kind      string // pdf | html | arxiv | text | markdown
    Title     string
    Text      string
    Metadata  map[string]string
}

type Source struct { ID, URL, Kind, Description string }

type ResearchBrief struct {
    Topic    string
    Findings []Finding   // {Claim, Evidence, Citations}
}

type Critique struct { /* above */ }

type Decision struct {
    Source string  // "llm" | "systemone"
    Pass   bool
    Score  float64  // model score, if reported
    Reason string   // verdict rationale
}

type Episode struct {
    Topic   string
    Docs    []Document
    Brief   ResearchBrief
    Script  Script
    Critique Critique
    AudioPath string
}
```

---

## 8. Error handling & limits

- **Fail fast on startup**: config errors, unreachable LLM endpoint (optional
  ping), required providers missing.
- **Per-stage failures**: a doc conversion failure → logged, pipeline continues;
  a search failure → agent notes it, continues; a tool error mid-loop → agent
  sees the error message and may retry or proceed.
- **Iteration guardrail**: `max_iterations`, `max_research_steps`, global time
  budget (default ~10 min); graceful shutdown.
- **TTS cost control**: per-call character cap; cache across reruns; print total
  characters (estimated cost) before rendering; `--dry-run` renders script/critique
  only (no TTS spend).
- All user-facing errors carry context (`stage: message`).

---

## 9. Testing strategy

| Layer | Test |
|---|---|
| Config | load/validate; env expansion; invalid configs rejected |
| Documents | golden tests: PDF (bundled test fixture), HTML, arXiv-style page |
| LLM client | `httptest` server returning canned chat/tool-call responses |
| Pipeline | mock LLM implementing scenario scripts (write → critique(pass/fail)); verify loop exits on pass & on cap; verify feedback passed to next write |
| Script writer | prompt snapshots; word-count target computation |
| TTS/audio | segment chunking; WAV assembly correctness (byte-level header + PCM size) |
| CLI | end-to-end with mock server (no network), asserts artifacts exist |

No tests should require real network or real LLM endpoints.

---

## 10. Milestones (proposed order)

1. **Skeleton**: CLI, config loader + validation (incl. `critic.*`), artifact writer. `podcast --help`.
2. **LLM client**: chat + tool calling + constrained decoding (`response_format`/GBNF) with retries; smoke test against a local endpoint.
3. **Document module**: converters + golden tests.
4. **Research agent**: brief generation (`--dry-run`, mocked tools).
5. **Critic + decision model**: independent `critic.llm`; constrained-decoding critique; `decision: "llm"`; then `decision: "systemone"` adapter (pending contract).
6. **Pipeline**: writer/critic loop; `--dry-run` end-to-end with mock.
7. **TTS + audio**: segment chunking, WAV concat, `podcast render`; cloud and local llama.cpp endpoints.
8. **Polish**: README, example config, example session, `make build/test`.

Each milestone lands as one commit; PRs not required (single-developer repo).

---

## 11. Risks & open questions (need your input)

1. **systemone endpoint contract** — exact URL shape / request / response schema,
   and what it should receive (full critique? topic + script?). It is not a public
   API I recognize, so the adapter (§6.5) is built to your spec once confirmed.
2. **Local TTS** — which model (Pocket / Kokoro / Qwen3-TTS / other)? On-device
   (Termux) or on a LAN machine? Is the llama.cpp build you run TTS-capable (libmtmd)?
3. **Search provider default** — `none` (arXiv-only) or Brave/Tavily with a key?
4. **Runtime budget** — reasonable end-to-end wall-clock target? Affects `time_budget`.
5. **Audio format** — WAV default vs. MP3 via ffmpeg?
6. **Voice** — single narrator (MVP) confirm.
7. **Cache & output** — `./out/<slug>/` + `.podcasts-cache/` defaults; confirm.

---

## 12. What I will need from you before writing code

1. Pick/confirm the defaults in §11.
2. Confirm the MVP scope (§1) and milestones (§10).
3. Repo hygiene: license? (none yet), naming of binary, any CI desire.
