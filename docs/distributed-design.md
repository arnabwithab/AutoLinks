# AutoLinks as a Distributed System

> Design of AutoLinks deployed on AWS as multiple cooperating services.
> Read top to bottom: it follows work through the system, then covers
> what happens when parts of it break.

---

## 0. The system in one picture

Two kinds of services — **API** and **worker** — each a set of identical
containers (ECS Fargate tasks) spread across availability zones. API
tasks answer requests behind a load balancer. Worker tasks do the slow
crawling. Everything they share — job queue, job records, link graph,
rate-limit counters — lives in ElastiCache. Vectors live in Qdrant
Cloud. Inference comes from a SageMaker endpoint with warm, autoscaling
replicas. The frontend stays on Vercel. Nothing important lives inside
any single container, so any container — or any whole AZ — can die and
the system carries on.

```
Vercel ──► ALB ──► API tasks (ECS ×N, multi-AZ) ──► ElastiCache (queue, jobs, graph, limits)
                                                      ▲
Worker tasks (ECS ×M, multi-AZ) ── pull jobs, write progress
   ├──► SageMaker endpoint (GLiNER + MiniLM, shared with API tasks)
   └──► Qdrant Cloud (vectors)
Secrets: Secrets Manager. Logs: CloudWatch.
```

---

## 1. Two paths: a fast one and a slow one

Every request takes one of two paths. Waiters take orders and serve;
cooks cook; tickets travel between them on a rail (ElastiCache).

- `POST /recommend` is the **fast path** (~1s). An API task handles it
  directly, start to finish. No queue, no worker, no job record.
- `POST /ingest` is the **slow path** (minutes). An API task writes a
  job record, drops it on the queue, and returns a `job_id` at once. A
  worker task does the crawl while the client polls for progress.

The split is the whole architecture: interactive work fails fast and
retries; background work queues and survives crashes. Different jobs,
different machinery.

---

## 2. The slow path, step by step

**Submit.** API task validates the sitemap URL, hashes it, and checks
for an existing job with that fingerprint — a resubmitted sitemap
returns the existing `job_id` instead of starting a duplicate crawl. New
job goes to the ElastiCache stream as `queued`, with a trace ID
attached. Milliseconds, done.

**Claim.** A worker task pulls the job and claims it with a lease token,
but only if its status is still `queued` — a conditional write, so two
workers racing produce one winner and one polite loser. Status becomes
`processing`.

**Fan out.** The worker parses the sitemap, then canonicalizes every URL
(scheme + host + path, trailing slash stripped, query dropped), dedupes
the list, and feeds it exactly once into an in-memory channel. A bounded
pool of fetchers (five by default, up to twenty) pulls URLs from the
channel — each URL is therefore seen by exactly one fetcher, by
construction, not by checking. Each fetcher runs the full pipeline per
article: fetch HTML → extract text and internal links → chunk → embed
(one batch call to SageMaker) → clear stale points, upsert fresh ones to
Qdrant → report back its outbound links. Progress (`articles_done`) is
written to the job record as it goes; per-article failures accumulate
without stopping the crawl.

**Finish.** When the channel drains, the worker merges the crawl's
outbound links into the graph with atomic increments, marks the job
`done`, and publishes "graph updated." If zero articles succeeded, the
job fails and retries with backoff; after repeated failures it moves to
the dead-letter queue with its error history, for a human to inspect and
re-queue.

**Why duplicates are harmless.** Delivery is at-least-once: a worker
that finishes but dies before acknowledging gets its job reclaimed and
re-run. Safety comes from idempotency at every layer — deterministic
point IDs (URL + chunk index) make re-upserts overwrite identical
points, progress writes set the same number twice, and the graph merge
is last-writer-wins per URL, so re-merging a crawl yields the same
graph. Prevent where cheap (channel-once, fingerprint), tolerate where
expensive.

---

## 3. The fast path

Recommendations never touch the queue: nothing is written, nothing needs
recovering, and queueing an interactive request just converts a fast
error into an indefinite wait. One API task runs the whole pipeline —
extract entities, batch-embed the queries, fan out vector searches to
Qdrant in parallel, rerank against its local graph cache — and responds.
Any task can serve any request (auth is a per-request JWT, no sessions),
so a dead task costs the client one retry onto a healthy one.

The fast path reads exactly one piece of shared state: the link graph,
from a local cache refreshed every minute or on the "graph updated"
pub/sub message. Seconds-stale equity scores are correct for every
practical purpose. It is also rate-limited by a shared per-user counter
in ElastiCache — per-task counters would multiply the real budget by the
task count.

---

## 4. The three levels of workers

Only the worker side has internal structure, in three levels — and only
the third autoscales:

- **Level 1 — job slots, fixed per task.** Each worker task runs four
  goroutines, each owning one whole sitemap job. One task, four
  concurrent crawls, always. Set at deploy time, never changes with load.
- **Level 2 — URL fetchers, bounded per job.** Each running job spawns
  its own 5–20 fetchers, born with the job and dead when it drains.
  Worst case per task: 4 × 20 fetchers plus the 4 slots — under a
  hundred goroutines; a handful in practice.
- **Level 3 — tasks, elastic.** ECS adds and removes whole containers.
  System concurrency is tasks × slots × fetchers; the first two are
  constants, the third is the variable.

**Why 4:** picked, not derived — four crawls' worth of in-flight pages,
chunks, and embeddings fits a small task with headroom, and jobs arrive
when a human clicks "ingest," so four slots never serialize real
traffic. Re-derive once from task memory, then never think about it
again. **Why 5:** politeness — five concurrent fetches reads as a
reader, not a scraper, and still drains a sitemap in about a minute.
**Why 20:** self-defense — the knob is user-supplied, so the cap stops
one request from opening hundreds of connections at a stranger's server
from our IP. The default protects strangers; the cap protects us.

---

## 5. The model server fleet

SageMaker is the one shared serialized resource: every recommendation
calls it twice, every crawled article once, from both services. One
replica serves a small pool of execution slots off warm GPU memory and
micro-batches queued embeds (~10ms windows) into single forward passes —
ten requests of work for barely more than one. Capacity is replica
count, autoscaled on p99 latency, with a warm floor that never hits
zero (scale-up takes minutes; interactive traffic can't wait). Health
checks must be deep — "alive" isn't "model loaded." Retries are always
safe: inference is pure, no leases, no fencing.

The endpoint's queue is FIFO with no priority lanes, so priority lives
in the callers: **interactive wins, background yields.** API tasks hold
a small guaranteed slice of concurrent calls and never back off for the
user; workers take what's left and back off hard on slowness or errors.
A ten-minute crawl is invisible; a thirty-second recommendation is a
broken product. The response cache (same draft hash → no model call)
keeps the bill small.

---

## 6. Scaling: when, why, how

The signal is **backlog per task**: pending stream entries divided by
running worker tasks. It fires when that exceeds ~1 — a CloudWatch alarm
raises ECS desired count, new tasks join the consumer group and pull
jobs. It fires because a minutes-long crawl sitting twenty minutes
queued is a broken SLA, while ten idle tasks at 3am is burned money. It
scales back in on sustained zero backlog after a cooldown, with
scale-in protection so a task drains before termination — and if one is
killed anyway, its lease expires and the job is reclaimed. The API side
scales independently on requests-per-task. During the 1–3 minute
scale-up lag, the API sheds load early (429 past a queue-depth limit)
instead of accepting work the fleet can't finish yet.

**A burst, end to end.** Two worker tasks idle; twelve sitemaps land in
thirty seconds. Backlog per task is 6 — alarm fires, ECS grows to eight
tasks. 8 × 4 slots = 32 concurrent crawls against 12 jobs: everything is
picked up, spares idle until scale-in. Each crawl keeps its own five
fetchers per target site, so no single site sees a burst. Backlog hits
zero, cooldown passes, fleet drains back to two.

---

## 7. When things break

Each answer reuses an idea from above. **Worker dies mid-crawl** (crash,
scale-in, dead AZ — cause doesn't matter): its jobs sit unacknowledged
and are reclaimed after the timeout; idempotency makes the re-run
invisible. **Two workers grab one job:** lease-token compare-and-set
fences the loser off. **Job keeps failing:** dead-letter queue with
error history, human re-queues. **API task dies:** stateless — ALB
reroutes, client retries; even a crash between job creation and response
is safe via the input fingerprint. **AZ goes dark:** tasks were spread
across AZs, ALB drains the dead one in seconds, mid-flight jobs are
reclaimed like any other abandoned work — nothing special, that is the
point. **Deploy rolls out:** new revisions pass health checks before old
tasks drain; crawls on replaced workers are reclaimed and resumed.
**ElastiCache fails over or goes unreachable:** fail writes honestly
("try again later"), keep serving reads from Qdrant plus the last cached
graph. Dumber but up — decided per endpoint in advance. **Network
splits:** each side keeps doing what it can from its own state
(workers drain, API serves cache) and reconciles on heal by reading
current records. Nobody catches up; everybody just reads.

---

## 8. Seeing the whole system

One crawl touches five things (API task, queue, worker, endpoint,
Qdrant); "which part is slow" must take seconds to answer. One trace ID
per job, generated at the API and carried in the job record, every log
line, and every downstream call — a crawl reads as one story across
tasks. All tasks log structured output to CloudWatch; log files on
individual containers would be useless. Every task reports liveness ("I
am running") separately from readiness ("I can reach ElastiCache,
Qdrant, and the endpoint") — the ALB routes only to ready tasks, ECS
restarts only dead ones.

---

## 9. What we are deliberately not building

- **No consensus (Raft/etcd).** ElastiCache is the single source of
  truth; conditional writes plus leases settle every race. Consensus is
  for systems with no center; we have one.
- **No Kafka / MSK.** Peak load is single-digit stream events per
  second; Redis Streams handles thousands. A separate backbone earns its
  complexity at ~100× our scale.
- **No microservices for recommendations.** A stateless computation;
  splitting it adds network hops to the latency-critical path for
  nothing. It stays inside the API task.
- **No distributed transactions.** Every step is idempotent and
  retryable — correctness from "safe to repeat," not "all or nothing."
