# AGENT.md

## Project Overview
AutoLinks is a semantic internal-link generation tool for SEO teams and content publishers. It analyzes draft text, extracts named entities using GLiNER, finds semantically similar articles via Qdrant vector search, and returns link recommendations with equity-aware re-ranking that prioritizes orphan (under-linked) pages.

## Tech Stack
- Frontend: React 18 + Vite 5
- Backend: Go (chi router)
- Database: Qdrant (vector database, cosine similarity)
- Styling: Vanilla CSS (no framework)
- State Management: Zustand
- NER: GLiNER2 via HuggingFace Space (eros483/autolinks-models)
- Embeddings: all-MiniLM-L6-v2 (384-dim, via HF Space)
- Content Extraction: built-in HTML stripping (regex) with internal-link extraction
- Evaluation Judge: Groq LLM (llama-3.3-70b-versatile)

## Key Commands

### Backend
```bash
cd backend

# Development
go run ./cmd/server                    # dev server
go build -o server ./cmd/server        # build binary

# Formatting & Static Analysis
go fmt ./...                           # format (zero-config, compiler-own formatter)
go vet ./...                           # static analysis (Printf mismatches, bad tags, unreachable code)
golangci-lint run                      # comprehensive linting (config in .golangci.yml)

# Testing (always run with race detector for goroutine-heavy code)
go test ./...                          # all tests
go test -v -race ./...                 # verbose + race detector
go test -coverprofile=coverage.out ./...  # with coverage
go test -bench=. ./...                 # benchmarks (if any)
go test ./internal/...                 # core + handler tests only
go test ./eval/...                     # eval tests only

# Dependencies
go mod tidy                            # sync dependencies (always after adding imports)
```

### Frontend
```bash
cd frontend
npm run dev                      # dev server (port 3000)
npm run build                    # production build
npm run test                     # run tests (vitest)
npm run lint                     # lint (eslint)
```

## Directory Structure

```
AutoLinks/
├── frontend/
│   ├── src/
│   │   ├── components/          # reusable UI components (Editor, Card, Header, Layout, etc.)
│   │   ├── utils/               # helper functions (editor_highlight.js)
│   │   ├── store/               # Zustand state management (store.js)
│   │   ├── services/            # API call functions (api.js)
│   │   ├── index.css            # global styles
│   │   ├── App.jsx              # root component / view router
│   │   └── main.jsx             # entry point
│   ├── index.html
│   ├── vite.config.js
│   └── package.json
│
├── backend/
│   ├── cmd/
│   │   └── server/
│   │       └── main.go          # entry point (HTTP server + goroutine worker pool)
│   ├── internal/
│   │   ├── config/
│   │   │   └── config.go        # env var loading (godotenv + os.Getenv)
│   │   ├── logger/
│   │   │   └── logger.go        # structured file-based logging
│   │   ├── qdrant/
│   │   │   └── client.go        # Qdrant gRPC client + collection management
│   │   ├── extract/
│   │   │   └── entities.go      # GLiNER NER entity extraction via HF Space
│   │   ├── embed/
│   │   │   └── embeddings.go    # embedding generation via HF Space (no local fallback)
│   │   ├── search/
│   │   │   └── search.go        # Qdrant vector similarity search
│   │   ├── rerank/
│   │   │   └── rerank.go        # equity-aware re-ranking + link graph (Redis)
│   │   ├── ingest/
│   │   │   ├── chunk.go         # text chunking (sliding window)
│   │   │   ├── crawl.go         # sitemap crawl + built-in text extraction
│   │   │   ├── fetch.go         # SSRF-hardened outbound HTTP client
│   │   │   └── linkgraph.go     # link graph building from internal links
│   │   ├── jobs/
│   │   │   ├── manager.go       # Redis-backed job lifecycle (create/get/update/errors)
│   │   │   ├── worker.go        # 4-goroutine pool, bounded per-URL workers, retry+backoff
│   │   │   └── dlq.go           # dead letter queue (Redis list: dlq:ingest)
│   │   ├── auth/
│   │   │   └── middleware.go    # Clerk JWT auth middleware (fail-closed)
│   │   ├── handlers/
│   │   │   └── routes.go        # all 8 API endpoints (thin layer on core/)
│   │   └── models/
│   │       └── models.go        # request/response structs with JSON tags
│   ├── eval/
│   │   ├── latency/
│   │   │   └── main.go          # Eval 1: round-trip latency measurement (target <3s)
│   │   ├── precision/
│   │   │   └── main.go          # Eval 2: LLM-as-a-judge semantic accuracy (target >90% YES)
│   │   ├── equity/
│   │   │   └── main.go          # Eval 4: link equity distribution (Gini + orphan reduction)
│   │   └── throughput/
│   │       └── main.go          # Sitemap ingest throughput (target >2.5 articles/sec)
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile                # multi-stage build (golang:1.25 → alpine:3.21)
│
├── docs/
│   ├── features.json            # canonical feature tracker — always kept up to date
│   ├── design.md                # full architecture and design document
│   ├── api.md                   # API usage documentation
│   ├── deployment.md            # deployment guide (Qdrant Cloud, Render, Vercel)
│   ├── evals.md                 # evaluation metrics documentation
│   ├── frontend-design.md       # frontend design specification
│   └── migrate-to-go.md         # Python → Go migration plan
├── .env.example                 # committed, no secrets
├── .gitignore
├── README.md
└── AGENT.md
```

## Go Tooling & Type Safety

### Type Checking

Go has **no separate type checker** — the compiler is the type checker. Both `go build ./...` and `go vet ./...` perform full type checking. No mypy/pyright equivalent is needed. Type safety is enforced at compile time, which is part of every `go build` and `go test` invocation.

### Toolchain

| Tool | Purpose | Command | When |
|------|---------|---------|------|
| `go fmt` | Formatter | `go fmt ./...` | Before every commit |
| `go vet` | Static analysis (built-in) | `go vet ./...` | Before every commit |
| `golangci-lint` | Aggregated linter (20+ linters) | `golangci-lint run` | Before every commit |
| `go test -race` | Race detector | `go test -race ./...` | Always — goroutine-heavy code |

### golangci-lint Configuration

A `.golangci.yml` at `backend/` enables these linters:

```yaml
linters:
  enable:
    - errcheck      # unchecked errors
    - gosimple      # code simplification
    - govet         # same as go vet
    - ineffassign   # dead assignments
    - staticcheck   # deep static analysis
    - unused        # unused vars, imports, functions
    - bodyclose     # leaked HTTP response bodies
    - gosec         # security issues
    - revive        # style rules (replaces deprecated golint)
    - misspell      # typos in identifiers and comments
    - gocritic      # opinionated code quality checks
    - gocyclo       # complex functions (>15 cyclomatic complexity)
    - prealloc      # preallocate slices where possible
    - unconvert     # unnecessary type conversions
    - wastedassign  # variable reassigned before use

linters-settings:
  gocyclo:
    min-complexity: 15
  revive:
    rules:
      - name: exported
        severity: warning
        disabled: false
      - name: blank-imports
      - name: context-as-argument
      - name: error-strings
      - name: error-naming
      - name: receiver-naming
      - name: increment-decrement
      - name: range

issues:
  exclude-dirs:
    - vendor
    - eval  # eval scripts are standalone binaries, less strict
```

## Conventions

### Go (Backend)
- **Package manager: `go mod`** — use `go mod tidy` to sync dependencies. No external package manager needed.
- **Formatter: `go fmt`** — run before every commit. No arguments, no configuration.
- **Linter: `golangci-lint`** — run before committing. Config in `.golangci.yml` if needed.
- Every Go file starts with a header comment: `// ----- <4-5 word purpose> @ backend/internal/<module>/<file>.go -----`
  - Example: `// ----- user authentication logic @ backend/internal/extract/entities.go -----`
- Naming: **camelCase** for unexported, **PascalCase** for exported, **snake_case** for file names.
  - Variables/functions/fields: `camelCase` (unexported) or `PascalCase` (exported)
  - File names: `snake_case.go`
- Imports: grouped (stdlib, external, internal) — `go fmt` handles ordering.
- API handlers are thin: validate input → call internal package → return JSON.
- `internal/` packages have zero knowledge of HTTP — they take plain structs, return plain structs.
- Env vars are accessed exclusively via the config package (`config.Get()` or equivalent) — never use `os.Getenv` directly.
- All logging uses the custom logger package (`logger.Info()`, `logger.Error()`) — never use `fmt.Println` or stdlib `log`.
- All packages under `internal/` are private to the module — the `cmd/server` binary is the only public entry point.

#### Naming Table

| Scope | Convention | Example |
|-------|-----------|---------|
| Packages | single lowercase word, no underscores | `extract`, `embed`, `search` |
| Exported types | PascalCase | `RecommendRequest`, `Entity` |
| Exported functions | PascalCase | `ExtractEntities()`, `EquityNeed()` |
| Unexported functions | camelCase | `normalizeEntityText()`, `getRedis()` |
| Variables/fields | camelCase | `minCharLength`, `inboundLinkCount` |
| Constants (exported) | PascalCase | `DefaultEntityLabels`, `JobTTL` |
| Constants (unexported) | camelCase | `maxConcurrent`, `dlqKey` |
| File names | snake_case | `entities.go`, `link_graph.go` |
| JSON tags | snake_case (matches API contract) | `json:"exact_phrase"` |
| Single-method interfaces | `-er` suffix | `EntityExtractor`, `Writer` |
| Error values | `err` or `Err` prefix | `err`, `ErrNotFound` |

#### Comment Conventions (GoDoc)

- Every **exported** symbol (functions, types, constants, variables) MUST have a doc comment starting with the symbol's name:

  ```go
  // Package extract provides NER entity extraction via the HF Space GLiNER2 endpoint.
  package extract

  // ExtractEntities sends text to the HF Space and returns named entities with position offsets.
  // It returns nil if the models space URL is unset or dry_run mode is enabled.
  func ExtractEntities(text string) ([]Entity, error) { ... }

  // Entity represents a named entity extracted from text with its position and label.
  type Entity struct {
      Text  string `json:"text"`
      Start int    `json:"start"`
      End   int    `json:"end"`
      Label string `json:"label"`
  }
  ```

- **Unexported** identifiers only need comments when the logic isn't self-documenting.
- Package doc comments go in a `doc.go` file or on the `package` line in any file.

#### Error Handling Conventions

- Always check errors. Never use `_` to discard an error unless intentional and explained in a comment.
- Wrap errors with `%w` (not `%v`) so callers can use `errors.Is` / `errors.As`:

  ```go
  result, err := extract.ExtractEntities(text)
  if err != nil {
      return nil, fmt.Errorf("failed to extract entities: %w", err)
  }
  ```

- Error messages are lowercase and have no trailing punctuation.
- The `errcheck` linter catches unchecked errors. The `gosimple` linter catches redundant error checks.
- Return `nil, err` pattern — never return partial results alongside an error.

### JavaScript (Frontend)
- camelCase for variables and functions
- PascalCase for components and types
- snake_case for file names
- All backend API calls go through `services/`, never directly in components
- API base URL is read from `VITE_API_BASE_URL` env var via `getApiBaseUrl()` in `services/api.js`

### General
- Commits: conventional commits format (feat:, fix:, chore:, docs:, test:, refactor:)
- Env vars: never committed, always have a .env.example with keys but no values
- API versioned from day one under /api/v1/
- **README badges**: READMEs should include HTML shield badges (via [shields.io](https://shields.io)) for things like build status, version, license, and tech stack. Use raw HTML `<img>` tags, not Markdown image syntax, so badge layout and alignment can be controlled.

## Development Philosophy
- TDD first: write the test, then the implementation. Never skip.
- Tests mirror the structure of the module they test (co-located `*_test.go` files)
- No function ships without a test
- API handlers are thin — logic lives in `internal/` packages
- Explicit over clever — readable code beats smart code

## Agent Roles

This project uses a three-agent workflow. Every task goes through all three stages.

- **Planner**: breaks down the task, identifies edge cases and risks, defines what tests need to exist, produces a written plan. Writes no code. Must check /docs for any relevant design documents before planning.
- **Builder**: implements exactly per the plan — no scope creep, no improvising. Writes tests first, then implementation.
- **Reviewer**: checks correctness, `go fmt` compliance, naming conventions, test coverage, and edge cases. Flags anything that deviates from this AGENT.md. Verifies that docs/features.json has been updated to reflect the work done.

The Planner must finish before the Builder starts.
The Reviewer must approve before any task is considered done.

## Agent Guidelines
- Always run `go fmt ./...` before considering Go code done
- Always run `go vet ./...` and `golangci-lint run` before considering Go code done
- File names use `snake_case.go`; exported symbols use `PascalCase`; unexported use `camelCase`
- Every exported symbol (functions, types, constants, variables) MUST have a GoDoc comment starting with the symbol's name
- Never modify files in /docs unless explicitly asked
- Always run tests after making changes — if tests fail, fix before moving on
- Always run tests with `-race` flag for goroutine-heavy code
- Every new Go file must start with the header comment — Reviewer should flag any file missing it
- Never use `os.Getenv` directly — always use the internal config package
- Never use `fmt.Println` or stdlib `log` — always use the internal logger package
- Never put API calls directly in React components — they belong in services/
- Always use `go mod tidy` after adding dependencies — never edit go.mod manually
- Always check /docs for relevant design documents before starting any task — if a design doc exists for what you're building, it takes precedence
- If a design doc is missing but the task is significant enough to warrant one, flag it to the user before proceeding
- Always update docs/features.json after completing any task — mark features as done, update test status, add new features if they were introduced. Follow the schema shape as defined in the existing docs/features.json.
- If something feels out of scope, flag it rather than silently doing it

## Project-Specific Notes

### External APIs
- **GLiNER2**: NER entity extraction via HuggingFace Space (`eros483/autolinks-models`). Auth via `HF_TOKEN`. Use `DRY_RUN=true` to skip API calls during development.
- **Embeddings**: Generated via the same HF Space (`POST /embed` endpoint). No local model loaded in-process.
- **Groq**: LLM judge for evaluation (Eval 2). API key stored in `GROQ_API_KEY`.

### Infrastructure
- **Backend**: Deployed on Render via multi-stage Dockerfile (Go build → alpine runtime). Free tier with cron-job.org keepalive. URL: `https://autolinks-api.onrender.com`
- **Frontend**: Deployed on Vercel. URL: `https://autolinks.vercel.app`
- **Vector DB**: Qdrant Cloud (free serverless tier) via gRPC (port 6334) or local via Docker (`docker run -p 6333:6333 -p 6334:6334 qdrant/qdrant`)
- **Redis**: Upstash (free tier) for link graph storage, job state tracking, and dead letter queue.

### Non-Standard Setup
- Backend is a single Go binary. Run `go run ./cmd/server` from the `backend/` directory, or `go build -o server ./cmd/server && ./server`.
- Frontend `.env` lives at the repo root (not in `frontend/`). `vite.config.js` reads it from `..` via `envDir`.
- Qdrant must be running before the backend starts. On startup, the server calls `ensureCollection()` to create the `articles` collection if it doesn't exist.
- The goroutine worker pool starts automatically with the HTTP server — no separate worker process is needed.

### Files/Directories Never to Touch
- `backend/go.sum` — managed by `go mod`, never edit manually
- `frontend/package-lock.json` — managed by npm
- `frontend/dist/` — build output, never edit directly
- `logs/` — auto-generated log files
- `qdrant_storage/` — local Qdrant data directory

### Known Gotchas
- The `internal/` directory enforces Go's visibility rules — packages under `internal/` cannot be imported by modules outside this repo. This is by design.
- HF Space SSE polling uses `net/http` with the same "event: complete" / "event: error" protocol as the Python backend. The Gradio API call/poll cycle is identical.
- Sitemap ingestion for large corpora (150+ articles) fans URLs out to a bounded set of worker goroutines sized by `max_concurrent` (default 5, clamped to 20) to avoid rate-limiting the source server.
- The equity eval can run in synthetic mode (`--mode synthetic`) without a live API — useful for CI/testing.
- Render free tier has a 512MB RAM limit. With no local model loaded (all inference via HF Space), the Go binary uses ~25MB RAM at runtime — well within the limit.
- Qdrant Cloud uses gRPC on port 6334. When running Qdrant locally for development, ensure both ports 6333 (HTTP) and 6334 (gRPC) are exposed: `docker run -p 6333:6333 -p 6334:6334 qdrant/qdrant`.
- The link graph is stored in Redis under key `autolinks:link_graph` and is restored on startup. If Redis is unavailable, the server starts with an empty graph.
