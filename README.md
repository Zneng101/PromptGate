# PromptGate

> Single-binary Go service · OpenAI-compatible prompt gateway with gray rollout

PromptGate is a **zero-dependency** prompt management and gray-routing gateway. It packs *template rendering → gray routing → streaming forwarding → audit logging* into a single executable with a built-in Web UI, ready to run out of the box.

- **Runtime**: single Go binary (no middleware, no external database)
- **Storage**: SQLite (embedded, stores template versions and audit history) + in-memory cache (hot data)
- **Frontend**: React + Vite + Tailwind, build output embedded into the binary via `go:embed`
- **Core interfaces**: OpenAI-compatible proxy endpoint + local Web UI

---

## 🎯 What it does & why use it

The core problem PromptGate solves: **how to ship prompts to production in a safe, controllable, observable way**. It pulls prompts out of your code and turns them into online-managed, gray-releasable, auditable resources.

### What it can do

- **Unified prompt management**: create, edit and version prompt templates in the Web UI — no code changes, no redeployments.
- **Variable rendering**: templates support `{{.var}}` placeholders, filled at runtime from request variables. Rendering happens locally with zero latency.
- **Gray rollout for prompts**: attach multiple versions to one template and split traffic by weight (e.g. 10% experiment / 90% stable) to compare results.
- **OpenAI-compatible**: clients need no changes — just point `base_url` at PromptGate. Streaming and non-streaming both work as-is.
- **Real-time cost observation**: tokens are estimated locally during streaming, with input/output cost trends shown live on the frontend.
- **Full audit**: every call's template, version, model, tokens and status is persisted to SQLite — traceable and replayable.
- **Offline development**: with no API key configured, Mock mode lets you preview rendering in the Playground — write prompts for free.

### Typical scenarios

| Scenario | How to use it |
| --- | --- |
| 🔬 Prompt A/B testing | Attach v1/v2 to one template at 50/50 weights, compare effect and cost from the audit log. |
| 🐛 Gray rollback | Release a new prompt to 5% traffic first; if something goes wrong, drop its weight or disable it — takes effect in seconds via hot cache update. |
| 👥 Team collaboration | Prompt engineers tweak wording in the Web UI; developers just call `/v1/chat/completions` with a `template` name. Clean separation of concerns. |
| 💰 Cost governance | The audit log breaks down token usage per template/version to locate expensive prompts for optimization. |
| 🔁 Multi-model routing | Point different versions at different models (e.g. gpt-4o / gpt-4o-mini) to route by scenario and cut cost. |
| 🧪 Local dev & debugging | Mock mode lets frontend/client code iterate on rendering logic without a real API or network. |
| 🛡️ Prompt security | The anti-injection sandbox blocks users from escalating via variable names or injecting malicious template actions. |
| 📦 Private LLM gateway | Act as the team's unified LLM egress — centrally manage templates, keys and audit, deployed as a single file with zero ops. |

> In one line: **PromptGate = Git for prompts + gray release + API gateway**, all in one binary.

---

## ✨ Features

| Capability | Description |
| --- | --- |
| 🎛️ Template management | Named templates + multiple versions, edited online in the Web UI |
| 🧪 Gray routing | Weighted consistent hashing on trace_id/user_id — the same user always hits the same version |
| 🛡️ Security sandbox | Anti-injection template rendering: whitelisted fields, range/define/call disabled, missing fields fall back to zero values |
| 🌊 Streaming forwarding | SSE pass-through with real-time local token estimation (no network needed) |
| 🔥 Hot updates | Publishing a new version atomically swaps the cache pointer — no process restart required |
| 📝 Audit log | Asynchronously batched to SQLite, recording model, version and tokens for every call |
| 🧩 Mock mode | Test rendering in the Playground without configuring an API key |

---

## 🗂️ Directory structure

```
promptgate/
├── cmd/
│   └── promptgate/           # main entry + auto config generation
├── internal/
│   ├── gateway/              # core proxy logic (gray routing + SSE)
│   ├── engine/               # template rendering engine (security sandbox)
│   ├── cache/                # in-memory cache + atomic update
│   ├── store/                # SQLite CRUD + audit log
│   └── web/                  # embedded static files (embed) + REST API
├── pkg/
│   └── tokenizer/            # token estimation algorithm (offline)
├── webui/                    # React + Vite + Tailwind frontend source
├── Makefile / build.ps1      # one-shot build
└── go.mod
```

---

## 🚀 Quick start

### Option 0: Download a prebuilt binary (simplest)

Go to the [Releases page](https://github.com/Zneng101/PromptGate/releases/latest), download the archive for your platform, extract and run:

```bash
# Linux / macOS
tar -xzf promptgate-linux-amd64.tar.gz && ./promptgate

# Windows
# Extract promptgate-windows-amd64.zip, then double-click promptgate.exe
```

### Option A: Build from source (recommended)

```bash
git clone https://github.com/Zneng101/PromptGate.git
cd promptgate

# Linux / macOS
make build && ./promptgate

# Windows
powershell -ExecutionPolicy Bypass -File .\build.ps1 -Run
```

On first launch, `config.yaml` and `data/promptgate.db` are generated automatically and a sample prompt is seeded.

### Option B: Run directly

```bash
go run ./cmd/promptgate
# or specify a port
go run ./cmd/promptgate --port 8080
```

Then open **http://localhost:8080**

> With no API key configured, PromptGate enters **Mock mode** — you can test prompt rendering in the Playground without a real key. Once a real key is set, the proxy takes effect immediately.

### Development mode (hot reload, frontend + backend)

```bash
# Terminal 1: backend
make dev-go          # listens on :8099

# Terminal 2: frontend (Vite HMR, proxies /api and /v1 to :8099)
make dev-web         # listens on :5173
```

---

## ⚙️ Configuration

`config.yaml` (auto-generated on first launch):

```yaml
server:
  host: "0.0.0.0"
  port: 8080
openai:
  api_key: ""                              # empty => Mock mode
  base_url: "https://api.openai.com/v1"
database:
  path: "./data/promptgate.db"
log_level: "info"
```

> The data directory defaults to `./data` under the current working directory, avoiding writes to the system disk. Use `-config` to specify a custom config file path.

---

## 🧠 Core algorithms

### 1. Template rendering & anti-injection (security sandbox)

`internal/engine/engine.go` — built on Go `text/template`, with three layers of defense:

1. **Syntax-tree pre-check**: the parse stage rejects dangerous actions like `{{range}}`/`{{template}}`/`{{define}}`/`{{block}}`/`{{call}}`;
2. **Whitelisted key names**: variable names may only contain letters, digits, underscores and dots;
3. **Pure-data context**: `sanitizeMap` recursively strips values down to primitive types, rooting out any access to struct methods or arbitrary function calls via variables;
4. **Zero-value default**: the `missingkey=zero` option makes missing fields resolve to zero values instead of erroring.

### 2. Gray routing (stateless consistent modulo)

`internal/gateway/gateway.go` — weighted consistent hashing:

```go
// Hash key (trace_id/user_id) with FNV-1a 32-bit, then hit the version whose weight threshold matches.
// The same key always lands on the same version (consistency).
hashVal := fnv.New32a().write(key).sum32()
for v := range activeVersions {
    if hashVal % totalWeight < v.weight { return v }
}
```

Example: `key="user_123"` hashes to 67 — misses the 10% bucket, misses the 20% bucket, hits the 90% bucket.

### 3. Real-time streaming token estimation (offline)

`pkg/tokenizer/tokenizer.go` — hybrid approximate counting:

- ASCII characters: ~1 token per 4 characters (matches GPT's English encoding density)
- Chinese / non-ASCII runes: ~2 tokens
- Consecutive long English words get a 1.2x weight

Used only for frontend cost-trend display. Precise billing relies on the upstream API's `usage` field.

### 4. Local cache hot update (atomic swap)

`internal/cache/cache.go` — double pointer + RWMutex:

```go
func (c *Cache) AtomicUpdate(newData map[string]*PromptTemplate) {
    c.mu.Lock(); defer c.mu.Unlock()
    c.data = newData      // swap the pointer directly
    c.version++           // old data waits for GC; in-flight requests are unaffected
}
```

Publishing a new version swaps the entire map pointer — no process restart needed.

### Core flow

```
Request → extract trace_id → local cache lookup (atomic read)
    → template hit → render (anti-injection)
    → gray routing (consistent hash) → forward to real LLM
    → receive streaming response → real-time token estimation → return to client
    → async write SQLite audit log (batched)
```

---

## 📡 API

### OpenAI-compatible proxy

`POST /v1/chat/completions` — compatible with OpenAI Chat Completions, plus PromptGate extensions:

```jsonc
{
  "template": "code-review",       // optional: triggers rendering + gray routing on hit
  "variables": { "code": "..." },  // optional: template variables
  "user_id": "u123",               // optional: gray routing key
  "trace_id": "t456",              // optional: tracing
  "model": "gpt-4o-mini",
  "stream": true,
  "messages": [{ "role": "user", "content": "..." }]
}
```

When `template` is omitted, PromptGate acts as a plain OpenAI pass-through proxy.

### Management API

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/templates` | List all templates |
| POST | `/api/templates` | Create a template |
| GET/PUT/DELETE | `/api/templates/{id}` | Read/update/delete a template |
| POST | `/api/templates/{id}/versions` | Add a version |
| PUT/DELETE | `/api/versions/{id}` | Update/delete a version |
| POST | `/api/publish` | Reload the cache (hot update) |
| POST | `/api/render` | Render preview (no LLM call) |
| GET | `/api/audit?limit=100` | Audit log |
| GET | `/api/config` | Runtime config |
| GET | `/api/health` | Health check |

---

## 🧰 Tech stack

- **Backend**: Go 1.25+, `modernc.org/sqlite` (pure-Go SQLite, no CGO), `bluele/gcache`, `gopkg.in/yaml.v3`
- **Frontend**: React 19, Vite, Tailwind CSS v4, React Router
- **Embedding**: `go:embed` embeds the frontend build output into the binary

---

## 📄 License

[MIT](./LICENSE)
