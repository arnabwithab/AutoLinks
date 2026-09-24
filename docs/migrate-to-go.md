# AutoLinks Backend Migration: Python → Go

> A comprehensive plan for migrating the AutoLinks backend from Python/FastAPI to Go/chi with an in-process goroutine worker pool.

---

## 1. Motivation

The current Python backend uses FastAPI + Celery + Redis for its HTTP layer and async job processing. While functional, this stack has several pain points:

| Pain Point | Root Cause | Go Fix |
|---|---|---|
| **~400MB Docker image** | Python runtime + dependencies + sentence-transformers model | ~15MB static binary (multi-stage build, no local model) |
| **~200MB RAM idle** | sentence-transformers loaded in-process | ~25MB idle (no local model — all inference via HF Space) |
| **5-10s cold start** | Python interpreter + torch/model loading | <1s (compiled binary, no model load) |
| **2 deployment targets** | Web service + Background Worker on Render | **1 deployment target** (goroutine pool in same process) |
| **Celery complexity** | Separate broker, worker process, config, serialization protocol | Goroutine pool: channel + semaphore + retry = ~80 lines |
| **Python GIL** | Async I/O works but CPU-bound ops (regex, JSON) contend | Native concurrency, no GIL, compiled regex |
| **2 Python package managers** | uv + pip in Dockerfile creates duplication | Single `go mod` — no external package manager |

The frontend sees **zero difference** — the API contract is preserved identically.

---

## 2. Library Choices

### 2.1 Celery Replacement: Goroutine Pool (No External Library)

**Verdict: No Celery. No gocelery. No gopher-celery. Pure goroutine pool.**

Research findings:

| Option | Status | Issues |
|--------|--------|--------|
| `gocelery/gocelery` | **Abandoned** since Nov 2020 | 47 open issues, uses deprecated `redigo`, Celery protocol v1 only, no retry support, no result backend stability |
| `marselester/gopher-celery` | Active (Jul 2025) | Clean API, modern `go-redis`, but **no result backend** (by design) — would need manual result tracking |
| `hibiken/asynq` | Active (redis-based) | Overkill — full task queue framework for 1 task type |
| **Goroutine pool** | stdlib only | No external dependency, exactly fits our single-task workload |

Rationale:
- **Only ONE background task type exists:** `crawl_and_ingest` (sitemap crawl). No multi-queue routing, no task chaining, no scheduled/cron tasks.
- The current Dockerfile already runs Celery **in the same container** as uvicorn — no distributed advantage is being leveraged.
- Retry with exponential backoff, DLQ, and job status in Redis are trivially implemented in Go without any framework.

### 2.2 Content Extraction: go-trafilatura

| Library | F1-Score | Stars | Status | Notes |
|---------|----------|-------|--------|-------|
| `github.com/markusmobius/go-trafilatura` | **0.904** | 146 | Active (v2.0.0, May 2025) | Line-by-line port of Python trafilatura. 2.5x faster. Fallback extractors (go-readability, go-domdistiller). |
| `go-shiori/go-readability` | 0.881 | 942 | **Archived** (Dec 2025) | Readability.js port. Faster but lower accuracy. |
| `github.com/PuerkitoBio/goquery` | — | 15k | Active | jQuery-like CSS selectors for link extraction. Not a standalone content extractor. |

**Choice: `go-trafilatura`** — closest to current Python behavior (F1=0.904 vs Python's 0.908), actively maintained, supports fallback.

### 2.3 HTTP Router: chi

| Library | Stars | Status | Fit |
|---------|-------|--------|-----|
| `github.com/go-chi/chi/v5` | 22.6k | Active (Jul 2026) | **Best: thin API layer, stdlib `http.Handler`** |
| `github.com/gin-gonic/gin` | 88.9k | Active (Feb 2026) | Heavier — full framework with custom context |

**Choice: `chi`** — matches the "thin routes, logic in core/" convention. Every handler is a standard `http.Handler`, trivially testable with `httptest`.

### 2.4 Qdrant Client: Official gRPC

**Choice: `github.com/qdrant/go-client` v1.18.3** — official client using gRPC (port 6334). Must verify gRPC connectivity with Qdrant Cloud free tier before finalizing. If gRPC is blocked, fall back to REST via `net/http` + JSON (manual but lightweight).

### 2.5 Other Libraries

| Purpose | Library | Stars | Status |
|---------|---------|-------|--------|
| Redis client | `github.com/redis/go-redis/v9` | 20k+ | De facto standard |
| Redis testing | `github.com/alicebob/miniredis/v2` | 3.6k | Real in-memory TCP Redis |
| HTML parsing | `github.com/PuerkitoBio/goquery` | 15k | jQuery-like selectors |
| Test assertions | `github.com/stretchr/testify` | 25k+ | `assert` + `require` |
| Env loading | `github.com/joho/godotenv` | 8k+ | `.env` file support |
| Concurrency | `golang.org/x/sync/semaphore` | — | Official x/ library |

---

## 3. What Gets Removed

### 3.1 Deleted Files (Entire `backend/` directory rewritten)

```
backend/
├── main.py                          → backend/cmd/server/main.go
├── pyproject.toml                   → backend/go.mod + go.sum
├── uv.lock                          → (deleted — go.mod manages deps)
├── requirements.txt                 → (deleted)
├── Dockerfile                       → rewritten as multi-stage Go build
├── worker.Dockerfile                → (deleted — no separate worker)
├── worker.py                        → (deleted — goroutine pool in-process)
├── .python-version                  → (deleted)
├── .dockerignore                    → rewritten
├── core/
│   ├── celery_app.py                → (deleted — no Celery)
│   ├── tasks.py                     → internal/jobs/worker.go (goroutine pool)
│   ├── extract.py                   → internal/extract/entities.go
│   ├── embed.py                     → internal/embed/embeddings.go (no local fallback)
│   ├── search.py                    → internal/search/search.go
│   ├── rerank.py                    → internal/rerank/rerank.go
│   ├── ingest.py                    → internal/ingest/{chunk,crawl,linkgraph}.go
│   ├── jobs.py                      → internal/jobs/manager.go
│   └── dlq.py                       → internal/jobs/dlq.go
├── api/v1/routes.py                 → internal/handlers/routes.go
├── schemas/
│   ├── request.py                   → internal/models/models.go
│   └── response.py                  → internal/models/models.go
├── utils/
│   ├── config.py                    → internal/config/config.go
│   ├── logger.py                    → internal/logger/logger.go
│   └── qdrant.py                    → internal/qdrant/client.go
├── tests/                           → co-located *_test.go files
│   ├── test_api/test_routes.py      → internal/handlers/routes_test.go
│   ├── test_core/                   → internal/{extract,embed,qdrant,rerank,ingest,jobs}/*_test.go
│   └── test_eval/                   → eval/{precision,equity}/main_test.go
└── eval/
    ├── eval_latency.py              → eval/latency/main.go
    ├── eval_precision.py            → eval/precision/main.go
    └── eval_equity.py               → eval/equity/main.go
```

### 3.2 Removed Dependencies

| Python Package | Reason |
|---------------|--------|
| `celery` | Replaced by goroutine pool |
| `sentence-transformers` | No local model — HF Space is sole embedding source |
| `trafilatura` (Python) | Replaced by `go-trafilatura` |
| `beautifulsoup4` | Replaced by `goquery` |
| `fastapi` | Replaced by Go `net/http` + `chi` |
| `uvicorn` | Replaced by Go `net/http` |
| `pydantic` / `pydantic-settings` | Replaced by Go structs + JSON tags |
| `httpx` | Replaced by Go `net/http` |
| `requests` | Replaced by Go `net/http` |
| `groq` (Python SDK) | Replaced by Go `net/http` + `encoding/json` |
| `redis` (Python) | Replaced by `go-redis/v9` |
| `qdrant-client` (Python) | Replaced by `qdrant/go-client` |
| `black` | Replaced by `go fmt` |
| `pytest` / `pytest-asyncio` / `pytest-cov` | Replaced by `testing` + `testify` |
| `fakeredis` | Replaced by `miniredis/v2` |
| `python-dotenv` | Replaced by `godotenv` |

### 3.3 Dropped Functionality

| Feature | Reason |
|---------|--------|
| **Local embedding fallback** (`_embed_via_local`) | HF Space is the sole embedding source. Removes 200MB RAM overhead. `MODELS_SPACE_URL` becomes required (not optional). |
| **Celery worker deployment** | Goroutine pool runs in-process with HTTP server. Render Background Worker service eliminated. |
| **Swagger UI (/docs)** | No auto-generated OpenAPI from chi. API docs remain in `docs/api.md`. Can add an OpenAPI spec manually if needed. |

---

## 4. What Gets Added

### 4.1 New Go Module

```
backend/
├── cmd/server/main.go               # Entry point (HTTP + worker pool startup)
├── internal/
│   ├── config/config.go             # Config struct loaded from env vars
│   ├── logger/logger.go             # Structured file logger
│   ├── qdrant/client.go             # Qdrant gRPC client + ensureCollection()
│   ├── extract/entities.go          # NER via HF Space SSE polling
│   ├── embed/embeddings.go          # Embeddings via HF Space (no local model)
│   ├── search/search.go             # Qdrant Query gRPC + result mapping
│   ├── rerank/rerank.go             # Equity formula + Redis link graph
│   ├── ingest/
│   │   ├── chunk.go                 # Sliding window text chunker
│   │   ├── crawl.go                 # Sitemap parser + go-trafilatura extraction
│   │   └── linkgraph.go             # Internal link inversion → inbound counts
│   ├── jobs/
│   │   ├── manager.go               # Redis job CRUD (create/get/update/errors)
│   │   ├── worker.go                # 4-goroutine pool, semaphore, retry, DLQ
│   │   └── dlq.go                   # Redis list: dlq:ingest
│   ├── handlers/routes.go           # chi router, 8 endpoints, CORS
│   └── models/models.go             # All request/response structs
├── eval/
│   ├── latency/main.go              # Eval 1: 50 sequential POSTs, P50/P95/P99
│   ├── precision/main.go            # Eval 2: Groq LLM judge, strict JSON schema
│   └── equity/main.go               # Eval 4: Gini + orphan reduction, live + synthetic
├── go.mod
├── go.sum
└── Dockerfile
```

### 4.2 Goroutine Worker Pool Design

```
┌───────────────────────────────────────────────────┐
│                    main.go                          │
│  ┌─────────────────────────────────────────────┐  │
│  │  HTTP Server (chi, port 8000)               │  │
│  │  POST /api/v1/ingest/sitemap                │  │
│  │    → jobs.CreateJob() in Redis              │  │
│  │    → enqueue via buffered channel            │──┼──→ jobs chan (capacity 100)
│  └─────────────────────────────────────────────┘  │
│  ┌─────────────────────────────────────────────┐  │
│  │  Worker Pool (4 goroutines, starts at boot) │  │
│  │  ← reads from jobs chan                     │  │
│  │  → Acquires semaphore.Weighted(5)           │  │
│  │  → HTTP fetches with backoff (30s/60s/120s) │  │
│  │  → Updates Redis job status each cycle      │  │
│  │  → On max retries → push to DLQ (Redis)     │  │
│  └─────────────────────────────────────────────┘  │
│                                                     │
│  Redis:                                             │
│    autolinks:job:{id}  → job state JSON with TTL   │
│    dlq:ingest          → failed job list            │
│    autolinks:link_graph → inbound link counts       │
└───────────────────────────────────────────────────┘
```

**Worker lifecycle:**

1. Server starts → 4 goroutines launched, each loops on `jobs` channel
2. `POST /ingest/sitemap` → creates job in Redis → sends `Job` struct on channel → returns `{job_id, status: "queued"}` to caller
3. Worker picks up job → marks status "processing" in Redis → runs `processJob()`
4. `processJob()`: parse sitemap → for each URL, acquire semaphore → concurrent fetch+extract+embed+upsert with `semaphore.Weighted(5)` bounding concurrent outbound HTTP calls
5. On failure: exponential backoff (30s → 60s → 120s), retry up to 3 times
6. After max retries: mark job "failed", push to `dlq:ingest` Redis list
7. On success: build link graph, persist to Redis, mark job "done"

**Retry with exponential backoff:**
```go
func (wp *WorkerPool) processJobWithRetry(ctx context.Context, job Job) error {
    for attempt := 0; attempt <= wp.maxRetries; attempt++ {
        err := wp.processJob(ctx, job)
        if err == nil {
            wp.jobManager.UpdateJob(job.ID, map[string]any{"status": "done"})
            return nil
        }
        if attempt < wp.maxRetries {
            delay := wp.baseDelay * time.Duration(1<<uint(attempt)) // 30s, 60s, 120s
            time.Sleep(delay)
        }
    }
    wp.dlq.Push(job.ID, job.TaskName, job.Args, lastErr, wp.maxRetries)
    wp.jobManager.UpdateJob(job.ID, map[string]any{"status": "failed"})
    return lastErr
}
```

**Concurrency profile at steady state:**
- 4 persistent goroutines (workers)
- Each worker allows up to 5 concurrent outbound HTTP fetches (semaphore)
- Worst case: 20 simultaneous outbound HTTP connections (4 workers × 5 fetches) — but typically only 1 job processes at a time, so 5 concurrent at most
- All goroutines are Go's lightweight cooperatively-scheduled green threads — no thread-per-connection overhead

### 4.3 Database: No Changes

| Resource | Before (Python) | After (Go) | Change |
|----------|----------------|------------|--------|
| **Qdrant** | REST client (`qdrant-client`) | gRPC client (`qdrant/go-client`), port 6334 | Protocol change only. Same collection (`articles`), same vectors (384-dim cosine), same payloads. |
| **Redis** | `redis-py` | `go-redis/v9` | Same keys, same data formats. Link graph at `autolinks:link_graph`. Jobs at `autolinks:job:{id}`. DLQ at `dlq:ingest`. |
| **API Contract** | FastAPI + Pydantic → JSON | chi + Go structs → JSON | **Identical JSON shapes.** The frontend sees zero difference. |

### 4.4 New Go Module Dependency List

```
github.com/go-chi/chi/v5
github.com/go-chi/cors
github.com/go-chi/render
github.com/qdrant/go-client/qdrant
github.com/redis/go-redis/v9
github.com/markusmobius/go-trafilatura
github.com/PuerkitoBio/goquery
github.com/joho/godotenv
github.com/google/uuid
github.com/stretchr/testify          // test only (assert + require)
github.com/alicebob/miniredis/v2     // test only
golang.org/x/sync/semaphore
```

---

## 5. API Endpoints — Zero Breaking Changes

All 8 endpoints preserved identically. Request/response JSON shapes are byte-for-byte compatible.

| Method | Path | Go Handler | Python Equivalent |
|--------|------|-----------|-------------------|
| POST | `/api/v1/recommend` | `handleRecommend` | `recommend()` |
| POST | `/api/v1/ingest` | `handleIngest` | `ingest()` |
| POST | `/api/v1/ingest/sitemap` | `handleIngestSitemap` | `ingest_sitemap()` |
| GET | `/api/v1/ingest/status/{jobID}` | `handleIngestStatus` | `ingest_status()` |
| GET | `/api/v1/ingest/result/{jobID}` | `handleIngestResult` | `ingest_result()` |
| POST | `/api/v1/ingest/retry-dead` | `handleRetryDead` | `ingest_retry_dead()` |
| GET | `/api/v1/health` | `handleHealth` | `health()` |
| GET | `/api/v1/link-graph` | `handleLinkGraph` | `get_link_graph()` |

**Response schemas (Go structs):**

```go
type Recommendation struct {
    ExactPhrase      string  `json:"exact_phrase"`
    ContextSnippet   string  `json:"context_snippet"`
    SuggestedURL     string  `json:"suggested_url"`
    SimilarityScore  float64 `json:"similarity_score"`
    EquityNeedScore  float64 `json:"equity_need_score"`
    FinalScore       float64 `json:"final_score"`
    InboundLinkCount int     `json:"inbound_link_count"`
}

type RecommendResponse struct {
    Status          string           `json:"status"`
    LatencyMs       int64            `json:"latency_ms"`
    Recommendations []Recommendation `json:"recommendations"`
}
```

---

## 6. Dockerfile — Multi-Stage Go Build

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /server /server
EXPOSE 8000
CMD ["/server"]
```

| Metric | Python Dockerfile | Go Dockerfile |
|--------|-------------------|---------------|
| Build image | `python:3.13-slim` (~150MB) | `golang:1.23-alpine` (~70MB) |
| Final image | ~400MB | ~15MB |
| Build time | ~60s (uv sync + pip) | ~30s (go build) |
| Runtime RAM | ~200MB (sentence-transformers) | ~25MB |
| Cold start | 5-10s (Python + model load) | <1s (static binary) |
| Processes | 2 (uvicorn + celery worker) | 1 (single Go binary) |
| Render services | Web Service + Background Worker | Web Service only |

---

## 7. Test Migration

### 7.1 Test Count (52 → 52+)

All Python tests rewritten in Go using `testing` + `testify/assert` + `miniredis`. Tests are co-located with their target package as `*_test.go` files.

| Python Test | Go Test | Count | Mock Strategy |
|------------|---------|-------|---------------|
| `tests/test_api/test_routes.py` | `internal/handlers/routes_test.go` | 8 | `httptest.Server` wrapping chi router |
| `tests/test_core/test_extract.py` | `internal/extract/entities_test.go` | 6 | `httptest.Server` for HF Space SSE |
| `tests/test_core/test_embed.py` | `internal/embed/embeddings_test.go` | 7 | `httptest.Server` for HF Space. **No local model tests.** |
| `tests/test_core/test_qdrant.py` | `internal/qdrant/client_test.go` | 4 | Mock or test search logic in isolation |
| `tests/test_core/test_rerank.py` | `internal/rerank/rerank_test.go` | 7 | Pure logic — no external deps |
| `tests/test_core/test_ingest.py` | `internal/ingest/*_test.go` | 3 | `httptest.Server` for HTTP fetch |
| `tests/test_core/test_jobs.py` | `internal/jobs/manager_test.go` | 4 | `miniredis.RunT(t)` |
| `tests/test_core/test_dlq.py` | `internal/jobs/dlq_test.go` | 2 | `miniredis.RunT(t)` |
| `tests/test_eval/test_eval_precision.py` | `eval/precision/main_test.go` | 5 | `httptest.Server` for Groq |
| `tests/test_eval/test_eval_equity.py` | `eval/equity/main_test.go` | 8 | Pure logic + synthetic mode |

**New tests added:**
- **Worker pool retry/DLQ cycle** — inject failing job, verify 3 retries, verify DLQ push after exhaustion
- **Worker pool concurrency** — submit N jobs, verify all complete, verify semaphore bounds respected

### 7.2 Test Commands

```bash
cd backend
go test ./...                          # all tests
go test -v -race ./...                 # with race detector
go test -coverprofile=coverage.out ./...  # with coverage
go test ./internal/...                 # core + handler tests only
go test ./eval/...                     # eval tests only
```

---

## 8. Evaluation Scripts

All 3 evals rewritten as Go `main` packages. Same logic, same targets.

| Eval | Go Binary | Target | Method |
|------|-----------|--------|--------|
| Eval 1: Latency | `go run ./eval/latency` | max < 3000ms | 50 sequential POSTs, P50/P95/P99 |
| Eval 2: Precision | `go run ./eval/precision` | >90% YES | Groq LLM judge, 10 tripwire cases |
| Eval 4: Equity | `go run ./eval/equity` | Gini improved, orphans lifted | Live API or `--mode synthetic` |

---

## 9. Latency Expectation

Go should improve latency 15-30% over Python for the same workload:

| Step | Python | Go | Delta |
|------|--------|-----|-------|
| Entity extraction (HF Space HTTP) | 300-600ms | 300-600ms | Same (network-bound) |
| Embedding generation (HF Space HTTP) | ~100ms | ~100ms | Same (network-bound) |
| Qdrant query | 50-100ms | 50-100ms | Same (network-bound) |
| JSON serialization | ~10-20ms | ~1-2ms | Go is 5-10x faster |
| Regex (chunking, link extraction) | ~5-10ms | ~2-5ms | Go is ~2x faster |
| In-memory scoring/sorting | ~5ms | ~2ms | Go is ~2x faster |
| **Total (warm)** | **~500-855ms** | **~470-820ms** | **~5-10% faster** |

The biggest wins come at the tails: Go's compiled JSON and regex eliminate Python's worst-case behavior, so P99 should improve more dramatically than mean.

---

## 10. Infrastructure Changes

### 10.1 Render

| Before | After |
|--------|-------|
| **Web Service**: `python:3.13-slim`, starts uvicorn + celery | **Web Service**: `golang:1.23-alpine → alpine:3.21` (multi-stage), starts single Go binary |
| **Background Worker**: `python:3.13-slim`, celery worker | **Deleted** — goroutine pool runs in web service process |
| Env vars: `PYTHONPATH`, `PYTHONUNBUFFERED` | Removed. Only app-level vars remain. |

### 10.2 Upstash Redis

No changes. `REDIS_URL` (possibly `rediss://`) is used directly by `go-redis/v9` which handles TLS natively.

### 10.3 Qdrant Cloud

No changes to cluster or collection. Must verify gRPC access (port 6334) with free tier. If needed, add an HTTP REST fallback layer.

### 10.4 CI/CD (GitHub Actions)

```yaml
# .github/workflows/ci.yml — backend job
jobs:
  backend:
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - run: go test -race ./...
        working-directory: backend
      - run: go vet ./...
        working-directory: backend
      - uses: golangci/golangci-lint-action@v6
```

### 10.5 Keepalive

No changes. `GET /api/v1/health` still responds identically. cron-job.org keepalive continues as before (GitHub Actions keepalive workflow has been removed).

---

## 11. Migration Order

```
Phase 1:  Scaffold (cmd/server/main.go, config, logger, qdrant client, go.mod)
Phase 2:  Core modules (extract, embed, search, rerank)
Phase 3:  Ingest modules (chunk, crawl with go-trafilatura, linkgraph)
Phase 4:  Models (request/response structs)
Phase 5:  Jobs (manager, worker pool, DLQ)
Phase 6:  HTTP handlers (chi router, all 8 endpoints, CORS)
Phase 7:  Tests for phases 2-6 (write alongside, not after)
Phase 8:  Eval scripts (latency, precision, equity)
Phase 9:  Dockerfile + deployment config
Phase 10: Documentation (AGENTS.md, design.md, deployment.md, api.md, features.json)
Phase 11: Delete Python backend/ directory
Phase 12: Update CI/CD (.github/workflows/ci.yml)
Phase 13: Deploy and run all 4 evals against production
```

Each phase is independently testable. The API contract never changes — old and new backends can coexist at different URLs during cutover.

---

## 12. Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Qdrant gRPC blocked by Qdrant Cloud free tier | Medium | Verify gRPC connectivity first. If blocked, implement REST fallback via `net/http` + JSON. |
| go-trafilatura extraction differs from Python trafilatura | Low | F1=0.904 vs 0.908 is nearly identical. Test against Wait But Why corpus and compare output. |
| HF Space SSE polling differs from Python | Low | Same HTTP protocol — "event: complete" / "event: error" SSE format. Test with mocked server. |
| Redis TLS (`rediss://`) handled differently | Low | `go-redis/v9` `redis.ParseURL()` natively supports `rediss://` with TLS. Test with Upstash. |
| Latency regression | Low | Go should be faster, not slower. Run Eval 1 before/after cutover to confirm. |
| goroutine pool crashes silently | Medium | Worker pool has panic recovery + logs crashes. Failed jobs go to DLQ. Health endpoint reports pool status. |

---

## 13. Success Criteria

| Criterion | Target | Measurement |
|-----------|--------|-------------|
| API compatibility | 100% JSON shape match | Run frontend test suite against Go backend |
| Test coverage | >= 52 tests passing (CURRENT 52 tests) | `go test ./...` |
| Eval 1: Latency | Max < 3000ms (current target), **expected improvement** | `go run ./eval/latency` |
| Eval 2: Precision | >90% YES rate | `go run ./eval/precision` |
| Eval 4: Equity | Gini improved, orphans lifted vs baseline | `go run ./eval/equity` |
| Image size | <20MB | `docker images` |
| RAM idle | <50MB | Render dashboard |
| Cold start | <2s | Timed deployment |
