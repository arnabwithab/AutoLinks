# AutoLinks — System Design

> What this system does, why it's built the way it is, and how data flows through it.

---

## 1. The Problem

When a publisher writes a new article, they need to add internal links to other pages on their site. These links pass authority through the site, help Google discover pages, and keep readers engaged. The standard workflow is:

1. Remember what relevant articles exist in the archive (requires encyclopedic knowledge of the site)
2. Search the CMS by keyword for each candidate topic
3. Open each candidate page to verify topical relevance
4. Pick the best destinations and write the links in the draft

This is slow, relies on human memory, and produces structurally biased results — authors link to pages they remember, which are the already-popular pages. Orphan pages stay orphaned because nobody remembers them at authoring time.

Existing automation tools attack step 2 with keyword matching. This fails twice: it cannot distinguish "Apple" the company from "Apple" the fruit, and it cannot connect "gradient descent" to a page about "backpropagation" because the words don't overlap.

The real insight is that internal linking is a **resource allocation problem**, not just a relevance problem. Every link you add to a page increases its inbound link count. A system that always links to the most relevant article will reflexively concentrate links on already well-linked pages, creating a rich-get-richer dynamic that starves orphan pages of crawler attention and ranking potential. The question is not just "which page is most relevant" but "given that every link is a finite allocation of authority, where should this link go to create the healthiest distribution across the entire site?"

---

## 2. The System in One Sentence

AutoLinks receives draft text, extracts named entities, finds semantically related articles in a vector database, and re-ranks candidates using an equity-aware formula that balances relevance against inbound-link scarcity — returning a clean list of linking suggestions that agents and humans can act on.

---

## 3. Architecture at a Glance

```
User's Browser (Vercel)
       │  HTTPS + Clerk JWT
       ▼
Go API Server (Render)
       │
       ├──► HF Space (GLiNER2 + MiniLM)  ── entity extraction, embeddings
       ├──► Qdrant Cloud (gRPC)          ── vector similarity search
       └──► Upstash Redis (TLS)          ── link graph, job state, dead letter queue
```

Three external services. The Go process carries no model weights and no local vector store. At idle it uses ~25MB RAM. Total infrastructure cost: $0 across free tiers.

The critical design constraint this architecture solves: **Render's free tier has a 512MB RAM limit.** A single ML model — even a compact one like GLiNER2 at 205M parameters — would consume most of that budget. Offloading all inference to a separate HuggingFace Space keeps the orchestrator lightweight enough to run comfortably within the limit while also separating concerns: the inference layer can be upgraded, rate-limited, or replaced independently of the business logic.

---

## 4. The Request Lifecycle

Follow a single user query from paste to response. Every decision in this flow is annotated with *why* it exists.

### 4.1 Auth Boundary

The user is authenticated by Clerk on the frontend. Before any API call, the frontend requests a short-lived JWT from Clerk's token endpoint. This token is attached as a `Bearer` header on every request.

When the Go server receives a request, a chi middleware extracts the token and verifies it against Clerk's SDK. If verification fails, the middleware returns 401 before the handler ever sees the request. This is a **trust boundary** — below this point, all code can assume the request is authenticated.

If `CLERK_SECRET_KEY` is not configured (local development), the middleware is not attached and all requests pass through anonymously. The decision to make auth conditional rather than always-on was pragmatic: development without Clerk credentials should work out of the box with `DRY_RUN=true`. The tradeoff is that the production deployment must always configure Clerk.

A second boundary exists at the HTTP body level. All POST endpoints are wrapped in a body size limiter that rejects payloads over 1MB. This is defense-in-depth — the Go binary can handle modest payloads, but unbounded input opens a denial-of-service vector even on free-tier infrastructure where RAM is scarce.

### 4.2 Entity Extraction

The handler receives the text and calls the named entity recognition pipeline. This is the first external service call in the request path, and it's the most latency-sensitive because everything downstream depends on its output.

**Why GLiNER:** Unlike traditional NER systems that detect from a fixed ontology (person, org, location, date), GLiNER takes a *user-provided list of labels* and finds spans matching any of them. This is the critical capability: the system can be instructed to look for "technology," "concept," and "topic" entities — categories that map to the kinds of phrases people link to. A traditional NER trained on CoNLL-2003 would find persons and organizations, missing the "gradient descent" and "spatial computing" entities that are the actual linking targets.

The label set was chosen experimentally: `person`, `organization`, `topic`, `technology`, `concept`. These five categories catch broad entity types while avoiding overly specific labels that would fragment extraction. The labels are not configurable by the user because tuning label sets is a model-level concern, not a per-request concern.

**Why HF Space:** GLiNER2 requires the model weights to be loaded in process. Loading a 205M parameter model into the Go binary would violate the 512MB Render limit. A dedicated HuggingFace Space running on HF's free CPU tier hosts both GLiNER2 and MiniLM behind a unified Gradio API. The Space loads models once at startup and serves them across all requests. This is a deployment decision, not an architectural one — the entity extraction interface (`ExtractEntities`) has no knowledge of where the model lives, only that `CallSpace` provides it.

**SSE polling protocol:** The Gradio Space API works through a call-and-poll pattern. The initial POST returns an `event_id`. The caller then GETs `/{endpoint}/{event_id}` repeatedly until the server emits `event: complete` with the result. This protocol exists because Gradio runs model inference asynchronously — the POST queues the work and the GET returns the result when ready. The polling loop uses exponential backoff starting at 500ms with a cap at 5 seconds and a maximum of 30 attempts (~30 seconds total). These values were tuned against observed HF Space latency: a warm Space completes extraction in 300–600ms, so the first few polls usually succeed. The backoff exists for cold starts and contention, not for the common case.

**Dry run mode:** When `DRY_RUN=true`, the entire HF Space call is bypassed and hardcoded fixture entities are returned. This exists so developers can work on search, re-ranking, and frontend integration without waiting for HF Space latency or consuming API credits. The fixture entities are matched against the draft text by case-insensitive substring, so different draft texts produce different fixture subsets — making dry-run testing somewhat realistic without being a full NER pass.

### 4.3 Entity Post-Processing

Raw GLiNER output contains noise: very short entities ("AI," "ML"), overlapping spans, and near-duplicates. The post-processing pipeline applies three filters in sequence, each with a specific purpose:

**Minimum character length (default: 5):** Short entities make poor linking anchors. A two-character entity like "AI" appears so frequently in technical text that making it a link would create dozens of link opportunities per page — overwhelming the linking UX and diluting the value of each link. The 5-character threshold was chosen by examining GLiNER's output on test drafts: it eliminates acronyms and single-word noise while preserving multi-word phrases that make meaningful anchors.

**Normalized deduplication:** Entities are normalized by lowercasing and stripping non-alphanumeric characters, then deduplicated by their normalized form. This catches "Machine Learning" and "machine learning" as duplicates while preserving the original casing of the first occurrence.

**Initialism deduplication:** If a multi-word entity like "Artificial General Intelligence" appears, its initialism "agi" is computed. Any short, purely alphabetic entity matching that initialism is then removed. This prevents the system from generating separate recommendations for both the full phrase and its acronym, which would create redundant Qdrant queries and potentially crowd out other entities from the recommendation set.

After filtering, the entities are trimmed to the top 10. The ordering follows GLiNER's original output order, which approximately corresponds to model confidence (earlier entities tend to have higher logit scores). Ten was chosen as the sweet spot: enough entities to generate meaningful recommendation diversity, few enough to keep the downstream embedding batch and parallel search within latency budget.

### 4.4 Query Construction

Each entity becomes a search query, but not by direct embedding of the entity text alone. Instead, the entity is concatenated with a 200-character prefix of the draft:

```
"gradient descent - The draft content about optimizing neural networks..."
```

This is a critical design choice. Embedding a bare entity like "gradient descent" would match any article about gradient descent, regardless of context. By prepending the draft's opening text, the embedding model produces a vector that is pulled toward articles discussing gradient descent *in a context similar to this draft.* The 200-character limit was chosen empirically: enough to carry topical signal without so much text that the entity itself is diluted. The opening text is used rather than the sentence containing the entity because entity position offsets are sometimes unreliable from GLiNER, and the opening 200 characters reliably contain the document's dominant topic.

### 4.5 Batch Embedding

All queries (up to 10) are sent to the HF Space in a single `EmbedBatch` call. This is the single most impactful latency optimization in the pipeline.

**Why batch matters:** Each individual call to the HF Space involves an HTTP round trip, a Gradio POST/poll cycle, and model inference. For 7 entities, that's 7 full round trips if done sequentially — potentially 3–5 seconds just for embedding. A batch call sends all texts at once; the Space runs MiniLM on the batch, and returns all vectors in one SSE stream. The total time approaches the time of a single call plus a small batch-processing overhead.

**Model choice:** `all-MiniLM-L6-v2` produces 384-dimensional vectors. It was chosen because it's the smallest model in the SentenceTransformers family that still produces semantically meaningful embeddings for paragraph-length text. The 384 dimensions are a fixed constant — the Qdrant collection is created with exactly this dimension, and any mismatch would cause upsert and query failures. A larger model like `all-mpnet-base-v2` would produce better embeddings at the cost of 768 dimensions (slower similarity computation) and a larger model footprint (reducing HF Space throughput on the free CPU tier). The tradeoff is accepted because internal link recommendations don't require the precision of a larger embedding model — the re-ranking step provides a second pass of discrimination.

### 4.6 Parallel Vector Search

For each entity's embedding vector, a goroutine queries Qdrant for the 100 most similar article chunks. All goroutines run in parallel.

**Why parallelism:** Qdrant gRPC queries are I/O-bound, not CPU-bound. Each query spends most of its time waiting for the network round trip to Qdrant Cloud and the vector comparison inside Qdrant. Running them concurrently overlaps the network latency — the total wall-clock time approaches the duration of the slowest individual query rather than the sum of all queries.

**Why 100 candidates per entity:** The re-ranker needs a wide candidate pool to exercise the equity-aware formula. If the search returned only 10 candidates per entity, the re-ranker would have little room to surface high-equity-need pages — the pool would be dominated by high-similarity (and typically well-linked) pages. At 100 candidates, there is meaningful headroom: the similarity threshold of 0.65 filters out noise, and the remaining pool contains a mix of high-similarity popular pages and moderate-similarity orphan pages. The re-ranker then picks winners from this diverse pool.

**Why Qdrant gRPC:** Qdrant Cloud exposes both REST (port 6333) and gRPC (port 6334) interfaces. gRPC was chosen because the Go client provides native protobuf types, which eliminates the JSON serialization overhead of REST. For a search returning 100 points with payload data, the protobuf binary format is measurably smaller and faster to deserialize than the equivalent JSON. The tradeoff is that protobuf tooling requires code generation and schema awareness, but the Qdrant Go SDK handles this transparently. Connection setup uses `sync.Once` to ensure exactly one gRPC client with connection pooling across the lifetime of the Go process — recreating the client per request would add TLS handshake overhead to every query.

**Nil payload safety:** Each Qdrant point carries a payload map with `url` and `chunk_text` fields. If these fields are missing or nil — which can happen if points were upserted without payloads or if Qdrant returns unexpected data — the system skips the point rather than panicking. This is a defensive measure against data corruption and API changes, not a normal-case code path.

### 4.7 Equity-Aware Re-Ranking

This is the intellectual core of the system. While the previous steps find candidates, this step decides which ones to recommend.

**The resource allocation framing:** A site has a fixed number of internal links distributed across its pages. Some pages receive many links (popular, well-referenced content), some receive few, and some receive none (orphans). When a new article is published and links are added, those links represent new incoming authority being allocated. A pure relevance system allocates authority to pages that are already semantically strong, which reinforces the existing distribution. An equity-aware system considers the current distribution when allocating, favoring pages that are relevant *and* underserved.

**The formula:**

```
equity_need(url) = 1 / (1 + inbound_links(url))
final_score      = α × similarity_score + (1 - α) × equity_need
```

`equity_need` is a decreasing function of inbound links. At 0 links it returns 1.0 (maximum need). At 50 links it returns ~0.02 (near saturation). The hyperbolic shape was chosen over linear decay because the marginal value of an additional link drops sharply — the first link to a page matters enormously, the tenth matters somewhat, the hundredth is noise. `1 / (1 + n)` captures this diminishing-returns curve with a single parameter.

`α` is the tuning knob. At `α = 1.0`, the system is pure similarity ranking (the baseline). At `α = 0.5`, relevance and equity have equal weight. The default of `0.7` gives a modest tilt toward similarity while leaving meaningful room for equity to influence ranking. The choice to expose `α` as a user parameter rather than fix it server-side is itself a design decision: different teams at different stages of their site's lifecycle need different weights. A team that just launched a redesign with 50 new pages needs equity emphasis (low α). A team maintaining a mature, well-linked site needs relevance emphasis (high α).

**Deduplication stages:** Before scoring, two deduplication passes run:

1. **Collapse to one per URL:** A single article appears as multiple Qdrant points (one per chunk). Multiple chunks from the same article scoring highly would crowd out other articles. The re-ranker keeps only the highest-scoring chunk per URL, ensuring each destination URL appears at most once in the candidate pool.

2. **Exclude already-selected URLs:** As entities are processed sequentially, URLs already assigned to earlier entities are excluded from consideration. This ensures the final recommendation set contains unique destination URLs — a single page is not suggested as the link target for multiple entities in one batch. The exclusion map is carried across the entity iteration loop.

**Example walkthrough:** Consider a draft containing "gradient descent" and "spatial computing" as entities. Qdrant returns:

| Entity | Candidate | Similarity | Inbound Links | Equity Need | Final (α=0.7) |
|--------|-----------|------------|---------------|-------------|----------------|
| gradient descent | Page A | 0.93 | 48 (very popular) | 0.02 | 0.657 |
| gradient descent | Page B | 0.85 | 0 (orphan) | 1.00 | 0.895 |
| spatial computing | Page C | 0.91 | 1 (underlinked) | 0.50 | 0.787 |

The pure similarity baseline (α=1.0) would rank: Page A (0.93) > Page C (0.91) > Page B (0.85). The orphan page loses.

The equity-aware system (α=0.7) ranks: Page B (0.895) > Page C (0.787) > Page A (0.657). The orphan page wins because its severe equity need (1.0) compensates for its slightly lower similarity (0.85 vs 0.93). This is the intended behavior — a modest relevance sacrifice for a meaningful equity gain.

### 4.8 Link Graph Infrastructure

The link graph is an in-memory `map[string]int` mapping each URL on the site to its inbound link count. The map is protected by `sync.RWMutex`: many readers (every recommendation request reads the graph for re-ranking) and one writer (ingestion replaces the graph wholesale). The choice of a single mutex over a concurrent map was deliberate: the graph is small (hundreds to low thousands of URLs), reads are extremely fast (map lookup), and contention is low because writes only happen during ingestion, not during normal traffic.

The graph survives restarts via Redis. On server startup, it is restored from key `autolinks:link_graph`. On ingestion completion, the new graph is persisted to the same key. The persistence path is best-effort — if Redis is unreachable, the graph is held in memory and operates normally for the server's lifetime. The next restart would lose the graph, but the server would still function (just without equity awareness until the next ingestion).

The Redis connection used by the link graph is separate from the one used by the job system. This is an artifact of package boundaries — the `rerank` package and `jobs` package each manage their own Redis client. Both use `sync.Once` for thread-safe lazy initialization, so only one connection pool exists per package.

---

## 5. The Ingestion Pipeline

The recommendation engine is useless without articles indexed in Qdrant and a populated link graph. Ingestion solves this cold start.

### 5.1 Why Async

A sitemap with 150 articles means 150 HTTP fetches, 150 HTML extractions, 150 chunking passes, 150 embedding calls, and 150 Qdrant upserts. Done sequentially with polite crawl delays, this takes 2–3 minutes — well past the 30-second timeout on Render's free tier. Done synchronously in a request handler, the caller's HTTP connection would time out.

The solution is an async job queue backed by a goroutine worker pool. The API endpoint returns immediately with a `job_id`. The crawl runs in background goroutines. The caller polls `/ingest/status/{job_id}` for progress.

### 5.2 Worker Pool Design

Four persistent goroutines block on a buffered channel (capacity 100). When a job is enqueued, one goroutine picks it up. The worker pool starts with the HTTP server and shares its process lifetime — no separate worker binary, no process manager, no Celery broker.

**Why goroutines over an external queue:** The Go runtime can multiplex thousands of goroutines onto OS threads efficiently. The ingestion workload is I/O-bound (HTTP fetches) with modest CPU (regex extraction, JSON serialization). Four workers is more than enough — the actual parallelism bottleneck is the semaphore limiting concurrent fetches to 5, not the number of workers consuming from the channel. A separate worker process (like the original Celery design) would require managing two Render services, coordinating through Redis, and dealing with process supervision. The goroutine pool eliminates all of this complexity: one process, one deployment, one lifecycle.

**Bounded concurrency:** Within each job, article processing uses `semaphore.Weighted(5)`. This limits simultaneous HTTP fetches to 5, preventing the system from overwhelming the target server with hundreds of concurrent connections. The number 5 was chosen as a conservative crawl rate: fast enough to complete ingestion in under a minute, slow enough to not trigger rate limiting or appear as aggressive scraping.

### 5.3 Retry and Failure Handling

Each job retries up to 3 times with exponential backoff: 30s → 60s → 120s. The base delay is aggressive (30 seconds) because ingestion failures are typically transient: a target server returns 503 under load, a DNS resolution glitches, a TCP connection resets. Short backoffs would retry into the same transient condition. Thirty seconds gives the remote server time to recover.

After 3 failures, the job is moved to the dead letter queue. The DLQ is a Redis list (`dlq:ingest`) storing serialized job metadata: original arguments, error message, timestamp, retry count. Failed jobs accumulate there for inspection — a human operator can check why specific articles failed and whether the errors are systemic (e.g., the target site is down) or isolated (e.g., a single malformed URL). The `/ingest/retry-dead` endpoint pops entries from the DLQ and re-enqueues them as fresh jobs, allowing manual bulk retry.

**What's not retried:** Parse failures on the sitemap XML return immediately — a broken sitemap won't fix itself. Empty text extraction is logged but not treated as an error — some pages genuinely have no extractable text (image galleries, interactive visualizations). These decisions reflect a tolerance for partial success: ingesting 148 of 150 articles is a successful run; blocking the pipeline for 2 broken articles is not.

### 5.4 Chunking Strategy

Articles are split into overlapping chunks using a sliding window: 5 sentences per chunk, stride of 3 sentences (2-sentence overlap).

**Why chunks:** Qdrant's similarity search compares the query vector against every stored point. If articles were stored as single points (one embedding per article), a 2000-word article would be represented by a vector that averages over disparate topics — the vector would point to "somewhere in the middle" of the article's full semantic range and match nothing precisely. Chunking creates multiple points per article, each capturing a focused semantic unit. A query about "gradient descent" matches the specific paragraph where gradient descent is discussed, not the entire article.

**Why sliding window overlap:** Chunks at strict sentence boundaries can split a topic across two chunks. If a 3-sentence explanation of backpropagation falls across a chunk boundary (sentences 4-5 in chunk 1, sentence 6 in chunk 2), neither chunk fully captures the topic. Overlap ensures that every sentence appears in at least two chunks, so topic boundaries are always fully contained within at least one chunk's window. The 2-sentence overlap with 5-sentence chunks means stride is 3 — each chunk shares 2 sentences with its neighbor.

**Sentence splitting:** A character-level scanner splits on `. ! ?` followed by whitespace. This is deliberately simple and handles English prose correctly for blog and article content. It does not handle abbreviations ("Dr.", "Mr.", "e.g."), ellipses ("..."), or decimal numbers ("3.14"). The tradeoff is accepted because:
1. The test corpus (Wait But Why) uses minimal abbreviations in body text
2. False splits create slightly shorter chunks, not missing content
3. The overlap means content near a false split still appears intact in an overlapping chunk
4. A full NLP sentence tokenizer would require loading a model, violating the no-local-models constraint

### 5.5 Link Graph Construction

During sitemap crawling, each page's HTML is scanned for internal `<a href>` links. These outbound links are inverted into inbound counts: for every page that links to `/blog/backprop`, the count for that URL increments by one.

**Normalization:** URLs from sitemaps and extracted links are normalized to a canonical form (`scheme://host/path` with trailing slashes stripped except for root). This catches the common pattern where sitemap URLs use `https://example.com/blog/post/` but internal links use `https://example.com/blog/post` or `http://example.com/blog/post/`. Without normalization, these would be treated as distinct URLs, fragmenting the link counts.

**Self-links are excluded:** A page linking to itself (common in navigation and breadcrumbs) is not counted as an inbound link. The count reflects external-to-the-page links only.

**External links are excluded:** Links to other domains pass through the filter but don't match any URL in the crawled set (the graph is seeded with only the sitemap URLs). These are logged as "unmatched targets" in the ingestion summary.

**Cross-validation logging:** After construction, the system logs: total URLs, total outbound links extracted, matched inbound targets, unmatched (external/non-sitemap) targets, zero-outbound pages (pages with no internal links at all), and orphan URLs (pages in the sitemap that no other page links to). A sample of top inbound URLs and unmatched targets is also logged. This logging serves as a data quality check — an unexpectedly high orphan count or zero-outbound count suggests a sitemap that includes non-article pages (author pages, category archives) that should be excluded.

---

## 6. Evaluation Strategy

The project includes standalone evaluation binaries that measure three properties of the system. Each eval answers a specific question about system behavior, and they are designed to be runnable against both a live deployment and (for the equity eval) synthetic data.

### 6.1 Latency (Eval 1)

**Question:** Can the system return recommendations fast enough for an interactive authoring workflow?

**Method:** 50 sequential POST requests to `/recommend` with diverse draft texts. Each request records the server-reported `latency_ms` (measured from handler entry to JSON serialization start on the server side). After 50 requests, the system computes mean, median (P50), P95, P99, min, max, and standard deviation.

**Target:** Maximum latency under 3000ms. On a warm instance, the pipeline is approximately:
- Entity extraction (HF Space): 300–600ms
- Batch embedding (HF Space): ~100ms
- Parallel Qdrant search (gRPC): 50–100ms per goroutine, overlapped
- Re-ranking (in-memory): <5ms
- Total: ~500–800ms

Cold starts on Render and HF Space are mitigated by the keepalive ping, but the eval is designed to catch regression in any single stage. If latency drifts above target, the per-stage instrumentation (each step logs its duration) identifies the bottleneck.

**Design note on measurement:** The eval measures server-reported latency rather than client-side wall clock time. This excludes network latency between the eval machine and Render, which varies by geography and connection quality. The reported numbers reflect the system's internal performance, not the user's network conditions. Client-measured latency is also recorded as a fallback if the server doesn't report latency, but the canonical metric is server-side.

### 6.2 Semantic Accuracy (Eval 2 — LLM-as-a-Judge)

**Question:** Are the recommended links actually relevant? Does semantic search outperform keyword matching on ambiguous terms?

**Method:** 50 draft texts drawn from Wait But Why's topic areas are passed through the API with `min_similarity=0.0` (widened to ensure candidates are returned). Each recommendation is sent to an LLM judge (Groq's `llama-3.3-70b-versatile`) with the prompt:

> Given this sentence context and the suggested link target page excerpt, is this a semantically accurate and highly helpful internal link suggestion? Reply YES or NO only.

The judge receives both the entity's surrounding context (from the draft) and an excerpt from the recommended page. It evaluates on three criteria: accuracy (does the topic match?), helpfulness (would a reader benefit from clicking?), and semantic fit (is the match meaning-level, not keyword-level?).

**Target:** >90% YES rate across all recommendations. Additionally, 5–10 "tripwire" cases are embedded in the test set — drafts containing ambiguous terms where keyword matching would fail (e.g., "Apple" in a food context should not link to a page about Apple Inc.). A >90% overall pass rate without correctly handling these tripwire cases is considered a FAIL. This prevents the system from appearing to pass by averaging out tripwire failures with easy cases.

**Why an LLM judge:** Manual evaluation of 50 drafts × 5–10 recommendations each is hours of human labor. An LLM provides cheap, fast, repeatable binary judgments. The choice of model matters less than the consistency of the prompt — the judge's quality depends on being given sufficient context (entity context + target excerpt) to make an informed decision. The free-tier Groq model was chosen for cost (zero marginal cost per eval run) and speed (Groq's inference is among the fastest available).

### 6.3 Link Equity Distribution (Eval 4)

**Question:** Does the equity-aware re-ranking actually distribute links more evenly than a pure similarity system? And does it do so without sacrificing semantic accuracy?

**Method:** The same 50 drafts are run through the system twice:

1. **Baseline (α = 1.0):** Pure similarity ranking. Equity is ignored.
2. **Equity-aware (α = 0.7):** The production default.

For each run, all recommended URLs are tallied. The resulting projected link graph (original counts + recommendations) is compared on two metrics:

- **Gini coefficient:** A measure of inequality in the link distribution. 0 = every page receives equal recommendations. 1 = one page receives all recommendations. Lower is better. The equity-aware system should produce a measurably lower Gini because it favors underserved pages.
- **Orphan reduction rate:** Percentage of currently-zero-inbound-link pages that received at least one recommendation. Higher is better. The equity-aware system should surface more orphans because their equity need of 1.0 gives them a scoring advantage over popular pages.

**Target:** Both conditions must hold:
- Equity-aware Gini < Baseline Gini
- Equity-aware orphan reduction > Baseline orphan reduction

If only one condition holds, the α parameter needs tuning. If neither holds, the equity formula is not discriminating effectively — either the link graph is too small for equity differences to matter, or the similarity score dominates too strongly at α=0.7.

**Synthetic mode:** The eval also supports a `--mode synthetic` flag that generates a realistic artificial link graph with controlled proportions (15% orphans, 35% low-linked, 30% mid-linked, 20% highly-linked). Each URL type is assigned a realistic similarity score range, and the same re-ranking formula is applied. This mode requires no live API and is useful for CI — it validates that the formula itself produces the expected ranking behavior independent of any external service availability.

---

## 7. Security Boundaries

Security in this system is not about protecting user data (there is none). It's about protecting infrastructure on free-tier services where a compromised endpoint could exhaust quotas or trigger service bans.

### 7.1 Authentication

Clerk JWT verification at the middleware layer gates all non-health endpoints. The token is verified against Clerk's public keys on every request. There is no session state on the server — each request carries its own proof of authentication. This is stateless auth, which matters for a free-tier service that can be restarted or migrated at any time.

### 7.2 TLS

All external connections use TLS: HF Space (HTTPS), Qdrant Cloud (gRPC with TLS), Upstash Redis (TLS on `rediss://`). TLS certificate verification uses the Go standard library's root CA pool. There is no certificate pinning — the services use public CA-issued certificates that rotate normally.

### 7.3 Input Validation

- POST bodies are limited to 1MB
- All JSON decoding validates the content type implicitly (malformed JSON returns 400)
- Required fields are checked explicitly in each handler
- Recommend text must be non-empty
- Entity text length is capped at 50 characters before query construction
- Context snippets are truncated to 150 characters in responses
- Entity list is trimmed to 10 before processing

### 7.4 Output Sanitization

- Internal error messages (`err.Error()`) are never returned to HTTP clients. Error responses contain user-facing messages only.
- Recommendation responses contain public URLs and text snippets, not database internals or stack traces.
- The frontend HTML-escapes all draft text before rendering highlights, preventing XSS through user input.
- The `suggested_url` field is rendered as a link with `rel="noopener noreferrer"` and `target="_blank"`, preventing reverse tab-napping.

### 7.5 CORS

The CORS middleware allows requests from explicitly configured origins (`FRONTEND_URL` env var, defaults to localhost:3000 and the Vercel deployment). The previous wildcard `*` was replaced because it's incompatible with credentialed requests and represents a configuration smell — CORS should be explicit about which origins are trusted.

---

## 8. Infrastructure Decisions

### 8.1 Go over Python

The original backend was Python/FastAPI with Celery workers. The Go rewrite was driven by three constraints:

1. **Memory:** Render's free tier has 512MB RAM. Python with local model inference (sentence-transformers) consumed ~400MB at idle, dangerously close to the limit. Go with zero local models idles at ~25MB.
2. **Concurrency:** Python's async/await and Celery require careful management of event loops and worker processes. Go's goroutines provide lightweight concurrency with no ceremony — a `go func()` call is sufficient to parallelize Qdrant search without managing thread pools.
3. **Deployment:** A single static binary with no runtime dependencies deploys trivially. No virtual environments, no pip install, no PyTorch wheels. The Dockerfile is a two-stage build producing a 15MB image.

### 8.2 Free Tier Sustainability

All four services operate on free tiers. Each tier has sleep/spindown behavior that must be worked around:

- **Render web service:** Spins down after 15 minutes of inactivity. A cron-job.org job pings the health endpoint every 10 minutes.
- **HF Space:** CPU Basic spaces also spin down. A second cron-job.org job pings the Space root endpoint.
- **Qdrant Cloud:** Serverless tier doesn't sleep but has rate limits. The Go client uses persistent gRPC connections to minimize connection overhead.
- **Upstash Redis:** Free tier has a 256MB storage limit and 1000 commands/day. The system uses Redis sparingly: a single key for the link graph, one key per active job (with 7-day TTL), and a single list for the DLQ.

### 8.3 Dockerfile Design

The multi-stage build separates the Go compilation environment from the runtime environment:

**Stage 1 (builder):** `golang:1.25-alpine` with full Go toolchain. Dependencies are cached via `go mod download` before copying source code. The binary is built with `CGO_ENABLED=0` (statically linked, no libc dependency) and `-ldflags="-s -w"` (stripped of debug symbols and DWARF tables).

**Stage 2 (runtime):** `alpine:3.21` with only `ca-certificates` (for HTTPS outbound connections) and `tzdata` (for timezone-aware logging). The binary from stage 1 is the only application file. The image is ~15MB.

This structure means the runtime image contains no build tools, no source code, no Go compiler, and no intermediate artifacts. It is the minimal viable runtime for a Go binary that makes HTTPS connections.

---

## 9. What's Not Here (Yet)

### 9.1 No Rate Limiting

There is no per-user or per-IP rate limiting. On a free-tier deployment serving a single user, this is irrelevant. For a multi-user system, rate limiting would need to be added at the middleware layer — likely token-bucket or sliding-window based, keyed by Clerk user ID from the auth context.

### 9.2 No Request Deduplication

If the same draft text is submitted twice, the system performs the full pipeline twice — including two HF Space calls and two Qdrant queries. There is no response cache. For a single-user tool, this is acceptable. For a multi-user system serving editing sessions where the same text is re-analyzed after minor edits, a content-addressed cache (hash the draft text + alpha + min_similarity as the cache key) would eliminate redundant computation.

### 9.3 No Per-User Isolation

Ingestion populates a single shared Qdrant collection and link graph. In a multi-tenant system, each user would need their own collection and graph — likely keyed by user ID with collection name templating.

### 9.4 No Link Graph Incremental Updates

The link graph is rebuilt from scratch on every sitemap ingestion. For a site publishing daily, running a full 150-article crawl to add one new article is wasteful. Incremental updates — crawling only new URLs from the sitemap's last-modified dates and merging them into the existing graph — would reduce ingestion time proportionally.

### 9.5 Deprecated Clerk SDK

The project uses `clerkinc/clerk-sdk-go`, which is archived and no longer maintained. Clerk recommends direct JWT verification against their JWKS endpoint. The current SDK still works, but migration to manual JWT verification would remove a dependency on an unmaintained library and reduce the attack surface to a single HTTP call per token verification.

### 9.6 No Component-Level Frontend Tests

The frontend has utility tests (editor highlighting, API helpers) but no component tests for Editor, Card, Recommendations, SitemapPage, or the Zustand store. The UI layer is verified manually. Component tests with React Testing Library would catch regressions in the highlight-to-card linking, error state rendering, and form submission flows.

---

*Last updated: 2026-08-05*
