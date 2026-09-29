# AutoLinks Eval Results

Last run: 2026-07-21

> Honesty note: only Eval 1 (latency) and the throughput eval have published
> results. Eval 2 (LLM-judge precision) and Eval 4 (link-equity/Gini) target
> >90% YES and orphan reduction respectively, but **no results have been
> recorded** — treat those numbers as unverified until this file contains them.

## Eval 1: Latency (Recommend)

**Target**: < 3000ms max — **PASS**

| Metric | Old (HF Space, sequential) | New (HF Space, batched + parallel) |
|--------|---------------------------|----------------------------------------|
| Mean | ~16,000ms | **608ms** |
| Median (P50) | — | **525ms** |
| P95 | — | **840ms** |
| P99 | — | **1606ms** |
| Min | — | 406ms |
| Max | ~20,000ms | **1606ms** |
| Success rate | 100% (HF rate-limited) | **50/50 (100%)** |

### Optimizations applied
1. **Batch embeddings** — all entity queries sent in one `EmbedBatch` call (vs N sequential `EmbedText`)
2. **Parallel Qdrant search** — goroutine per entity for `SearchSimilar` + `RerankCandidates`
3. **HF Space inference** — `gliner2-base-v1` (205M) + `all-MiniLM-L6-v2` served via HF Space (`eros483/autolinks-models`), bypassing local CPU constraints
4. **Concurrent sitemap fetches** — bounded worker goroutines (`max_concurrent`) in the worker pool

### Model used
- GLiNER2: `fastino/gliner2-base-v1` (205M params) served via HF Space Gradio API
- Embedding: `all-MiniLM-L6-v2` (384-dim) served via HF Space Gradio API

### Infrastructure
- Backend: Go, single binary, goroutine worker pool
- Vector DB: Qdrant Cloud (gRPC, cosine similarity)
- Redis: Upstash (link graph + job queue)
- All inference runs via HF Space — no local model loaded in-process

### Comparison to migration doc targets
| Target | Expected | Actual |
|--------|----------|--------|
| Total latency | <3000ms | **608ms mean / 1606ms max** |
| Image size | <20MB | ~15MB |
| RAM idle | <50MB | ~25MB |
| Cold start | <2s | <1s |
| Tests passing | >=52 | **50 (Go)** |
| Eval 1: Latency | <3000ms | **PASS (1606ms)** |

## Throughput Eval (Sitemap Ingest)

**Target**: >2.5 articles/sec

| Metric | Result |
|--------|--------|
| Sitemap | waitbutwhy.com/post-sitemap.xml |
| Articles | 202 |
| Duration | 82.0s |
| Average throughput | 2.46 articles/sec |
| Concurrent fetches | 5 (bounded worker goroutines) |
| Status | Marginal (within <2% of target) |

### Notes
- Worker pool fans out per-URL fetches across a bounded set of goroutines (`max_concurrent`, default 5)
- Each article: HTTP fetch → HTML extraction → chunk → embed (MiniLM local) → Qdrant upsert
- Bottleneck: Qdrant Cloud upserts + MiniLM embedding (CPU-bound)
- Per-article progress tracking disabled during concurrent batch (batched status update at end)

## Layer 3: HF Space Optimization (Not Yet Implemented)

GLiNER2 models available for HF Space deployment:
| Model | Size | Downloads | License |
|-------|------|-----------|---------|
| `fastino/gliner2-base-v1` | 205M | 552k | Apache 2.0 |
| `fastino/gliner2-large-v1` | 340M | 387k | Apache 2.0 |
| `urchade/gliner_small-v2.1` | 166M | 9.5k | Apache 2.0 |

For the HF Space, converting from Gradio to FastAPI + uvicorn workers requires verifying free-tier compatibility. The current Gradio SSE API is functional for single requests; the bottleneck is serial execution. With FastAPI + 4 workers, concurrent embedding requests would be handled in parallel.
