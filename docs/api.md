# AutoLinks API Guide

This guide explains how to run the API locally, what each endpoint expects, and what shape to expect back.

## Base URL

Local development:

```text
http://127.0.0.1:8000
```

Versioned API prefix:

```text
/api/v1
```

---

## Start The API

From the backend directory:

```bash
go run ./cmd/server
```

Environment variables are loaded from the project root `.env` file.

If you want to avoid external API calls during development:

```bash
DRY_RUN=true go run ./cmd/server
```

The server starts an HTTP server on port 8000 and an in-process goroutine worker pool for background sitemap ingestion jobs.

---

## Response Conventions

Successful recommendation responses return:

- `status`
- `latency_ms`
- `recommendations`

Validation errors usually return HTTP `400`.

Application errors usually return HTTP `500` with a `detail` field. Dependency failures return `502`/`503` (e.g. embedding service or job store unavailable), and rate-limited requests return `429`.

---

## `POST /api/v1/recommend`

Analyze draft text and return internal link recommendations.

### Request Body

```json
{
  "text": "CUDA optimization can dramatically speed up model training.",
  "alpha": 0.7,
  "min_similarity": 0.65
}
```

### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `text` | string | yes | Draft text to analyze |
| `alpha` | float | no | Similarity weight in the equity-aware rerank formula, `0`–`1` (omit for default `0.7`). `0` is valid and means pure-equity ranking |
| `min_similarity` | float | no | Minimum vector similarity score required before a candidate is surfaced, `0`–`1` (omit for default `0.65`). `0` is valid |

### Notes

- `alpha` and `min_similarity` are optional; an explicit `0` is honored, only omission falls back to the default. Values outside `0`–`1` return `400`.

### Example cURL

```bash
curl -X POST "http://127.0.0.1:8000/api/v1/recommend" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "Wait But Why is organizing a global meetup this weekend.",
    "alpha": 0.7,
    "min_similarity": 0.65
  }'
```

### Example Response

```json
{
  "status": "success",
  "latency_ms": 720,
  "recommendations": [
    {
      "exact_phrase": "Wait But Why",
      "context_snippet": "A post about the Wait But Why community and meetup planning.",
      "suggested_url": "https://example.com/wait-but-why-meetup",
      "similarity_score": 0.82,
      "equity_need_score": 0.5,
      "final_score": 0.724,
      "inbound_link_count": 1
    }
  ]
}
```

### Notes

- Short noisy entities are filtered before search.
- Abbreviation duplicates such as `WBW` and `Wait But Why` are deduplicated before query generation.
- Low-similarity candidates below `min_similarity` are not returned.

---

## `POST /api/v1/ingest`

Ingest a single article into Qdrant.

### Request Body

```json
{
  "url": "https://example.com/blog/cuda-optimization",
  "content": "CUDA optimization improves GPU throughput by reducing memory bottlenecks..."
}
```

### Example cURL

```bash
curl -X POST "http://127.0.0.1:8000/api/v1/ingest" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/blog/cuda-optimization",
    "content": "CUDA optimization improves GPU throughput by reducing memory bottlenecks..."
  }'
```

### Example Response

```json
{
  "status": "success",
  "chunks_ingested": 1
}
```

### Notes

- The API chunks article text before embedding and upserting to Qdrant.
- Stored payload includes the source URL and chunk text.

---

## `POST /api/v1/ingest/sitemap`

Crawl a sitemap, extract article content, build the internal link graph, and ingest the articles.

### Request Body

```json
{
  "sitemap_url": "https://example.com/post-sitemap.xml",
  "max_concurrent": 5
}
```

### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `sitemap_url` | string | yes | Sitemap to crawl (must be an http/https URL) |
| `max_concurrent` | integer | no | Max concurrent page fetches during crawl (default 5, clamped to 20) |

### Example cURL

```bash
curl -X POST "http://127.0.0.1:8000/api/v1/ingest/sitemap" \
  -H "Content-Type: application/json" \
  -d '{
    "sitemap_url": "https://example.com/post-sitemap.xml",
    "max_concurrent": 5
  }'
```

### Example Response

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued"
}
```

### Notes

- The crawl runs asynchronously; poll `/api/v1/ingest/status/{jobID}` for progress. This endpoint returns immediately with a `job_id`.
- Article text is extracted with `go-trafilatura` (regex fallback when it yields nothing).
- The same crawl parses internal `<a href>` links with `goquery`.
- Those links are inverted into inbound link counts and merged into the existing graph used by the equity-aware reranker.

---

## `GET /api/v1/ingest/status/{jobID}`

Check the status of an async sitemap ingestion job.

### Example cURL

```bash
curl "http://127.0.0.1:8000/api/v1/ingest/status/550e8400-e29b-41d4-a716-446655440000"
```

### Example Response

```json
{
  "status": "processing",
  "progress_pct": 45.0,
  "articles_done": 67,
  "total": 150,
  "errors": []
}
```

---

## `GET /api/v1/ingest/result/{jobID}`

Retrieve the final result of a completed sitemap ingestion job.

### Example cURL

```bash
curl "http://127.0.0.1:8000/api/v1/ingest/result/550e8400-e29b-41d4-a716-446655440000"
```

### Example Response

```json
{
  "status": "done",
  "chunks_ingested": 150,
  "duration_seconds": 82.0,
  "errors": []
}
```

---

## `POST /api/v1/ingest/retry-dead`

Re-enqueue permanently failed ingestion jobs from the dead letter queue.

### Example cURL

```bash
curl -X POST "http://127.0.0.1:8000/api/v1/ingest/retry-dead"
```

### Example Response

```json
{
  "retried_count": 3,
  "job_ids": ["abc-123", "def-456", "ghi-789"]
}
```

---

## `GET /api/v1/link-graph`

Returns the current inbound link graph used by the equity-aware reranker.

### Example cURL

```bash
curl "http://127.0.0.1:8000/api/v1/link-graph"
```

### Example Response

```json
{
  "status": "success",
  "url_count": 150,
  "link_graph": {
    "https://example.com/page1": 5,
    "https://example.com/page2": 0,
    "https://example.com/page3": 12
  }
}
```

---

## Authentication

Auth is fail-closed: the server refuses to start if `CLERK_SECRET_KEY` is unset unless `AUTH_DISABLED=true` is explicitly set. When auth is enabled, every endpoint except `/api/v1/health` requires a Clerk JWT `Bearer` token in the `Authorization` header. Requests without one receive `401`.

```bash
-H "Authorization: Bearer <clerk_session_token>"
```

---

## `GET /api/v1/health`

Health check with per-dependency status.

### Example cURL

```bash
curl "http://127.0.0.1:8000/api/v1/health"
```

### Example Response

```json
{
  "status": "ok",
  "qdrant": "ok",
  "redis": "ok",
  "models": "configured"
}
```

`status` is `ok` only when Qdrant and Redis are reachable; otherwise `degraded`. HTTP is always `200` (liveness).

---

## Common Workflow

1. Start Qdrant.
2. Start the Go server.
3. Ingest existing content with `/api/v1/ingest` or `/api/v1/ingest/sitemap`.
4. Send draft text to `/api/v1/recommend`.
5. Render the returned recommendations in your frontend or CMS integration.
