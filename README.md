<div align="center">

# AutoLinks

</center>

<center>
<p>
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8.svg?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/chi-v5-00ADD8.svg?logo=go&logoColor=white" alt="chi">
  <img src="https://img.shields.io/badge/Qdrant-1.18+-blue.svg" alt="Qdrant">
  <img src="https://img.shields.io/badge/React-18.2-blue.svg" alt="React">
</p>
</center>

<p>A semantic internal link generation API that analyzes draft text, extracts named entities using GLiNER, finds semantically similar articles via Qdrant vector search, and returns high-confidence internal linking recommendations with equity-aware re-ranking.</p>

---
<div align="left">

## Features

- **Named Entity Extraction** - Uses GLiNER to identify entities from draft text
- **Semantic Search** - Vector similarity search using embeddings
- **Equity-Aware Ranking** - Re-ranks recommendations to prioritize orphan pages
- **Sitemap Ingestion** - Crawl and index articles from any sitemap URL
- **Graph Diagnostics** - Logs orphan counts, top inbound URLs, and skipped sitemap targets for debugging link-graph quality

---

##  Local Usage

### 1. Install everything

```bash
make install
```

This scaffolds `.env`, downloads Go modules, and installs npm packages.

### 2. Configure `.env` and deploy ML Inference

Edit `.env` with your credentials (HF token, Qdrant URL, Redis URL, etc.).

```bash
make deploy-inference
```

Post deployment, add HF space URL to .env

### 4. Run

```bash
make run             # starts qdrant, backend (:8000), and frontend (:3000)
make stop            # kill all servers + stop qdrant
```

### 5. Lint, Test & Check

```bash
make check           # format + vet + lint + test
```

### 6. Build

```bash
make build           # Go binary at backend/server
```

For the full command reference (coverage, benchmarks, eval tests, tidy, etc.), see [AGENTS.md](AGENTS.md).

---

## Further Documentation

- Architecture Decisions: [docs/design.md](docs/design.md)
- Deployment strategies: [docs/deployment.md](docs/deployment.md)
- Full API usage, request examples, and response shapes: [docs/api.md](docs/api.md)
- AI Evals: [docs/design.md](docs/design.md)

---

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MODELS_SPACE_URL` | HF Space for GLiNER + MiniLM inference | - |
| `HF_TOKEN` | Hugging Face access token | - |
| `QDRANT_API_KEY` | Optional Qdrant API key for hosted deployments | - |
| `QDRANT_URL` | Qdrant endpoint (HTTP for local, HTTPS for cloud) | `http://localhost:6334` |
| `REDIS_URL` | Redis for job queue and link graph | - |
| `GROQ_API_KEY` | Groq LLM for precision eval | - |
| `DRY_RUN` | Skip HF Space calls, use fixtures | `false` |
| `DEBUG` | Enable debug mode | `false` |
| `RERANK_ALPHA` | Similarity weight for re-ranking | `0.7` |

---

## License

MIT
