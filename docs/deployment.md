# AutoLinks Deployment Guide

This guide covers the complete deployment process for AutoLinks, covering infrastructure setup, containerization, cloud deployment, and verification.

---

## Prerequisites

- [GitHub](https://github.com) account with the AutoLinks repository
- [Hugging Face](https://huggingface.co) account (free) for GLiNER2 + MiniLM inference Space
- [Qdrant Cloud](https://qdrant.tech) account (free tier)
- [Upstash](https://upstash.com) account (free tier) for Redis job queue
- [Render](https://render.com) account for backend hosting (free tier)
- [Vercel](https://vercel.com) account for frontend hosting (free tier)

---

## Step 1: Set Up Qdrant Cloud

### 1.1 Create a Qdrant Cloud Account

1. Go to [cloud.qdrant.io](https://cloud.qdrant.io) and sign up with GitHub or email
2. Verify your email if required

### 1.2 Create a New Cluster

1. Click **Create Cluster** in the Qdrant Cloud dashboard
2. Select the **Free** tier (sufficient for development)
3. Choose a region closest to your target audience (e.g., US East or EU West)
4. Name the cluster (e.g., `autolinks-cluster`)
5. Click **Create Cluster** and wait 1-2 minutes for provisioning

### 1.3 Get Connection Details

1. Once the cluster is ready, click on it to view details
2. Go to the **Overview** tab
3. Copy the following values:
   - **API Endpoint** (e.g., `https://xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.us-east-1-1.qdrant.tech`)
   - **API Key** (click to reveal if hidden)

### 1.4 Configure API Access (Optional)

1. Go to **Security** tab in the cluster dashboard
2. Ensure API key authentication is enabled (default)
3. Note: For free tier, no additional API key creation needed - use the default one provided

### 1.5 Update Local .env

Add your Qdrant credentials to the project `.env`:

```bash
QDRANT_URL=https://xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.us-east-1-1.qdrant.tech
QDRANT_API_KEY=your_qdrant_api_key_here
```

---

## Step 2: Backend Dockerfile (Go Multi-Stage Build)

The Go backend uses a multi-stage Dockerfile for a minimal production image (~15MB).

### 2.1 Project Setup

Ensure the backend directory has a valid `go.mod`:

```bash
cd backend
go mod tidy
```

### 2.2 Dockerfile

Create `backend/Dockerfile`:

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

### 2.3 .dockerignore

Create `backend/.dockerignore`:

```
*.go~
*.test
*.out
.env
.env.example
logs/
*.log
.git
.gitignore
*.md
eval/
```

---

## Step 3: Deploy Backend to Render

### 3.1 Push Code to GitHub

1. Ensure your code is committed to a GitHub repository
2. The repository should have the `backend/Dockerfile` at the root of the backend folder

### 3.2 Create a Web Service on Render

1. Log in to [Render Dashboard](https://dashboard.render.com)
2. Click **New** and select **Web Service**
3. Connect your GitHub account and select the AutoLinks repository
4. Configure the service:
   - **Name**: `autolinks-api`
   - **Region**: Select a region (e.g., Oregon - US West)
   - **Branch**: `main`
   - **Build Command**: Leave empty (Render auto-detects Dockerfile)
   - **Start Command**: Leave empty (already configured in Dockerfile)

### 3.3 Set Environment Variables

In the Render dashboard, add the following environment variables under **Environment Variables**:

| Variable | Value |
|----------|-------|
| `MODELS_SPACE_URL` | `https://eros483-autolinks-models.hf.space` |
| `HF_TOKEN` | Your Hugging Face access token |
| `QDRANT_URL` | Your Qdrant cloud endpoint with gRPC port (e.g., `https://xxxx.us-west-1-0.aws.cloud.qdrant.io:6334`) |
| `QDRANT_API_KEY` | Your Qdrant API key |
| `REDIS_URL` | Upstash Redis connection string (`rediss://default:<token>@<host>:6379`) |
| `GROQ_API_KEY` | Your Groq API key (for evaluation) |
| `DRY_RUN` | `false` (set to `true` to test without external API calls) |
| `DEBUG` | `false` |
| `RERANK_ALPHA` | `0.7` |

### 3.4 Deploy

1. Click **Create Web Service**
2. Render will build the Docker image and deploy (may take 3-5 minutes)
3. Once deployed, your backend will be live at `https://autolinks-api.onrender.com`

### 3.5 Verify Backend Health

Visit `https://autolinks-api.onrender.com/api/v1/health` — you should receive a JSON response indicating health status.

### 3.6 Keepalive for Cold Starts

The free Render tier spins down after 15 minutes of inactivity. To prevent cold starts (and keep the HF Space awake):

1. Go to [cron-job.org](https://cron-job.org)
2. Create a free account
3. Create three cron jobs, each scheduled every 10 minutes:
   - **Render**: `GET https://autolinks-api.onrender.com/api/v1/health` (timeout 120s)
   - **HF Space**: `GET https://eros483-autolinks-models.hf.space/` (timeout 120s)
   - **Qdrant**: `GET https://<qdrant-host>:6333/collections` with header `api-key: <QDRANT_API_KEY>` (timeout 60s)
4. These pings keep the services warm

> This replaces the old GitHub Actions keepalive workflow (`.github/workflows/keepalive.yml`), which has been deleted.

---

## Step 4: Configure Frontend Environment

### 4.1 Update .env for Frontend

Update the root `.env` file to point to your deployed Render backend:

```bash
VITE_API_BASE_URL=https://autolinks-api.onrender.com/api/v1
```

### 4.2 Create .env for Frontend Deployment

Create or update `frontend/.env.production` (or configure via Vercel dashboard):

```bash
VITE_API_BASE_URL=https://autolinks-api.onrender.com/api/v1
```

---

## Step 5: Deploy Frontend to Vercel

### 5.1 Connect Repository to Vercel

1. Go to [Vercel Dashboard](https://vercel.com/dashboard)
2. Click **Add New** → **Project**
3. Import your GitHub repository
4. Configure:
   - **Framework Preset**: Vite (or Auto-detect)
   - **Root Directory**: `frontend`
   - **Build Command**: `npm run build`
   - **Output Directory**: `dist`

### 5.2 Set Environment Variables

In Vercel project settings, add:

| Variable | Value |
|----------|-------|
| `VITE_API_BASE_URL` | `https://autolinks-api.onrender.com/api/v1` |

### 5.3 Deploy

1. Click **Deploy**
2. Wait 1-2 minutes for the build to complete
3. Your frontend will be live at `https://autolinks.vercel.app` (or your custom domain)

---

## Step 6: Verify Everything Works

### 6.1 Test Backend API

```bash
curl -X POST https://autolinks-api.onrender.com/api/v1/recommend \
  -H "Content-Type: application/json" \
  -d '{"text": "Deep learning has revolutionized CUDA optimization in modern GPUs."}'
```

Expected response:

```json
{
  "status": "success",
  "latency_ms": 500-1000,
  "recommendations": [...]
}
```

### 6.2 Test Backend Health Endpoint

```bash
curl https://autolinks-api.onrender.com/api/v1/health
```

Expected response: `{"status": "healthy"}` or similar

### 6.3 Test Frontend

1. Open `https://autolinks.vercel.app` in a browser
2. Enter some text in the editor
3. Click **Get Recommendations**
4. Verify that recommendations appear with suggested URLs

### 6.4 Test Sitemap Ingestion

```bash
curl -X POST https://autolinks-api.onrender.com/api/v1/ingest/sitemap \
  -H "Content-Type: application/json" \
  -d '{"sitemap_url": "https://waitbutwhy.com/post-sitemap.xml"}'
```

Note: This may take a few minutes for 150+ articles

---

## Step 7: Clean Up README

After deployment, update `README.md` to reflect production URLs:

1. Update the deployment badges:
   ```html
   <a href="https://autolinks-api.onrender.com">
     <img src="https://img.shields.io/badge/Render-Deployed-success?logo=render" alt="Render Deployment">
   </a>
   <a href="https://autolinks.vercel.app">
     <img src="https://img.shields.io/badge/Vercel-Deployed-black?logo=vercel" alt="Vercel Deployment">
   </a>
   ```

2. Update any references from `localhost` to production URLs in documentation

3. Update the environment variables table to reflect production values

4. Remove any local-only instructions (e.g., Docker run commands for local Qdrant)

---

## Environment Variable Summary

### Backend (Render)

| Variable | Description | Example |
|----------|-------------|---------|
| `MODELS_SPACE_URL` | HF Space for GLiNER2 + MiniLM | `https://eros483-autolinks-models.hf.space` |
| `HF_TOKEN` | Hugging Face access token | `hf_xxxx...` |
| `QDRANT_URL` | Qdrant Cloud endpoint | `https://xxxx.us-west-1-0.aws.cloud.qdrant.io` |
| `QDRANT_API_KEY` | Qdrant API key | `xxxxx...` |
| `REDIS_URL` | Upstash Redis for job queue | `rediss://default:xxx@host:6379` |
| `GROQ_API_KEY` | Groq LLM judge API key | `gsk_xxxx...` |
| `DRY_RUN` | Skip external API calls (for testing) | `false` |
| `DEBUG` | Enable debug logging | `false` |
| `RERANK_ALPHA` | Equity-aware ranking weight | `0.7` |

### Frontend (Vercel)

| Variable | Description | Example |
|----------|-------------|---------|
| `VITE_API_BASE_URL` | Backend API URL | `https://autolinks-api.onrender.com/api/v1` |

---

## Troubleshooting

### Backend

- **500 errors**: Check Render logs in the dashboard
- **Qdrant connection errors**: Verify `QDRANT_URL` and `QDRANT_API_KEY` are correct
- **Cold start delays**: Use cron-job.org to ping every 10 minutes
- **Ingestion jobs stuck**: Verify `REDIS_URL` is set and Upstash Redis is running; check worker pool logs for failed jobs
- **HF Space errors**: Verify `HF_TOKEN` is valid and `MODELS_SPACE_URL` is correct

### Frontend

- **CORS errors**: Ensure backend CORS allows your Vercel domain
- **API not reachable**: Verify `VITE_API_BASE_URL` matches your Render URL

### General

- **DRY_RUN**: Set `DRY_RUN=true` to test without calling HF Space
- **Index empty**: Run the sitemap ingestion endpoint to populate the vector database