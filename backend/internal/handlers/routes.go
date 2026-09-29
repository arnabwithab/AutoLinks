// ----- chi router, 8 endpoints, CORS @ backend/internal/handlers/routes.go -----
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/auth"
	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/embed"
	"github.com/arnabwithab/AutoLinks/backend/internal/extract"
	"github.com/arnabwithab/AutoLinks/backend/internal/ingest"
	"github.com/arnabwithab/AutoLinks/backend/internal/jobs"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/arnabwithab/AutoLinks/backend/internal/models"
	"github.com/arnabwithab/AutoLinks/backend/internal/qdrant"
	"github.com/arnabwithab/AutoLinks/backend/internal/rerank"
	"github.com/arnabwithab/AutoLinks/backend/internal/search"
	"github.com/arnabwithab/AutoLinks/backend/internal/trace"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

const (
	searchCandidateLimit = 100
	maxRecommendations   = 10
	defaultMinSimilarity = 0.65
	defaultAlpha         = 0.7
	defaultMinCharLength = 5
	maxRequestBodyBytes  = 1 << 20 // 1MB
	maxEntityQueryRunes  = 200
	maxSnippetRunes      = 150
	maxSitemapConcurrent = 20
	rateLimitPerMinute   = 120
)

// WorkerPool is the shared worker pool instance, set by main.go.
var WorkerPool *jobs.WorkerPool

// NewRouter creates and configures the chi router with all endpoints.
func NewRouter(tokenVerifier auth.TokenVerifier) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(trace.Middleware)
	r.Use(requestBodyLimiter(maxRequestBodyBytes))
	r.Use(rateLimitMiddleware(rateLimitPerMinute, time.Minute))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   strings.Split(config.FrontendURL(), ","),
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/", handleHealth)
	r.Get("/api/v1/health", handleHealth)

	r.Group(func(r chi.Router) {
		if tokenVerifier != nil {
			r.Use(auth.RequireAuth(tokenVerifier))
		}
		r.Post("/api/v1/recommend", handleRecommend)
		r.Post("/api/v1/ingest", handleIngest)
		r.Post("/api/v1/ingest/sitemap", handleIngestSitemap)
		r.Get("/api/v1/ingest/status/{jobID}", handleIngestStatus)
		r.Get("/api/v1/ingest/result/{jobID}", handleIngestResult)
		r.Post("/api/v1/ingest/retry-dead", handleRetryDead)
		r.Get("/api/v1/link-graph", handleLinkGraph)
	})

	return r
}

func requestBodyLimiter(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateLimitMiddleware is a per-IP fixed-window limiter. It is deliberately
// in-process: with more than one replica this needs Redis or an upstream limiter.
func rateLimitMiddleware(limit int, window time.Duration) func(http.Handler) http.Handler {
	var mu sync.Mutex
	counts := make(map[string]int)
	reset := time.Now().Add(window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			if time.Now().After(reset) {
				counts = make(map[string]int)
				reset = time.Now().Add(window)
			}
			ip := clientIP(r)
			counts[ip]++
			over := counts[ip] > limit
			mu.Unlock()

			if over {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("Failed to encode JSON response: %s", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"detail": message})
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func handleRecommend(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // request orchestration is inherently branchy
	var req models.RecommendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	alpha := defaultAlpha
	if req.Alpha != nil {
		alpha = *req.Alpha
		if alpha < 0 || alpha > 1 {
			writeError(w, http.StatusBadRequest, "alpha must be between 0 and 1")
			return
		}
	}

	minSimilarity := defaultMinSimilarity
	if req.MinSimilarity != nil {
		minSimilarity = *req.MinSimilarity
		if minSimilarity < 0 || minSimilarity > 1 {
			writeError(w, http.StatusBadRequest, "min_similarity must be between 0 and 1")
			return
		}
	}

	startTime := time.Now()

	rawEntities, err := extract.ExtractEntities(req.Text)
	if err != nil {
		if errors.Is(err, extract.ErrNoEntities) {
			writeError(w, http.StatusBadRequest, "No high-quality entities found in text")
			return
		}
		logger.Error("Entity extraction failed: %s", err)
		writeError(w, http.StatusInternalServerError, "Entity extraction failed")
		return
	}

	entities := extract.PostProcessEntities(rawEntities, defaultMinCharLength)
	if len(entities) == 0 {
		writeError(w, http.StatusBadRequest, "No high-quality entities found in text")
		return
	}
	repairEntityOffsets(entities, req.Text)

	trimmed := entities
	if len(trimmed) > 10 {
		trimmed = trimmed[:10]
	}

	prefix := truncateRunes(req.Text, maxEntityQueryRunes)
	var queries []string
	var validEntities []extract.Entity
	for _, e := range trimmed {
		if len(e.Text) > 50 {
			continue
		}
		queries = append(queries, e.Text+" - "+prefix)
		validEntities = append(validEntities, e)
	}

	if len(queries) == 0 {
		writeError(w, http.StatusBadRequest, "No high-quality entities found in text")
		return
	}

	allEmbeddings, err := embed.EmbedBatch(queries)
	if err != nil {
		logger.Error("Batch embed failed: %s", err)
		writeError(w, http.StatusInternalServerError, "Embedding service unavailable")
		return
	}
	if len(allEmbeddings) != len(queries) {
		logger.Error("Embedding count mismatch: got %d for %d queries", len(allEmbeddings), len(queries))
		writeError(w, http.StatusBadGateway, "Embedding service returned an unexpected number of vectors")
		return
	}

	type entityResult struct {
		entity     extract.Entity
		candidates []rerank.Candidate
	}

	resultCh := make(chan entityResult, len(validEntities))
	var wg sync.WaitGroup

	for i, e := range validEntities {
		wg.Add(1)
		go func(entity extract.Entity, embedding []float64) {
			defer wg.Done()

			candidates, err := search.SearchSimilar(embedding, searchCandidateLimit, minSimilarity)
			if err != nil {
				logger.Error("Search failed for entity '%s': %s", entity.Text, err)
				resultCh <- entityResult{entity: entity}
				return
			}

			var searchCandidates []rerank.Candidate
			for _, c := range candidates {
				searchCandidates = append(searchCandidates, rerank.Candidate{
					URL:       c.URL,
					ChunkText: c.ChunkText,
					Score:     c.Score,
				})
			}

			resultCh <- entityResult{entity: entity, candidates: searchCandidates}
		}(e, allEmbeddings[i])
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var allResults []entityResult
	for res := range resultCh {
		allResults = append(allResults, res)
	}

	var recommendations []models.Recommendation
	selectedURLs := make(map[string]bool)

	for _, result := range allResults {
		reranked := rerank.RerankCandidates(result.candidates, alpha, selectedURLs)

		for _, candidate := range reranked {
			if len(recommendations) >= maxRecommendations {
				break
			}

			recommendations = append(recommendations, models.Recommendation{
				ExactPhrase:      result.entity.Text,
				ContextSnippet:   truncateRunes(candidate.ChunkText, maxSnippetRunes),
				SuggestedURL:     candidate.URL,
				SimilarityScore:  candidate.Score,
				EquityNeedScore:  candidate.EquityNeedScore,
				FinalScore:       candidate.FinalScore,
				InboundLinkCount: candidate.InboundLinkCount,
			})
			selectedURLs[candidate.URL] = true
		}
	}

	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].FinalScore > recommendations[j].FinalScore
	})

	if len(recommendations) > maxRecommendations {
		recommendations = recommendations[:maxRecommendations]
	}

	latencyMs := time.Since(startTime).Milliseconds()
	logger.Info("Recommend completed in %dms", latencyMs)

	writeJSON(w, http.StatusOK, models.RecommendResponse{
		Status:          "success",
		LatencyMs:       latencyMs,
		Recommendations: recommendations,
	})
}

func repairEntityOffsets(entities []extract.Entity, text string) {
	for i := range entities {
		if idx := strings.Index(text, entities[i].Text); idx >= 0 {
			entities[i].Start = idx
			entities[i].End = idx + len(entities[i].Text)
		}
	}
}

func handleIngest(w http.ResponseWriter, r *http.Request) {
	var req models.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.URL == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "url and content are required")
		return
	}

	chunks, err := ingest.IngestArticle(req.URL, req.Content)
	if err != nil {
		logger.Error("Ingest error: %s", err)
		writeError(w, http.StatusInternalServerError, "Ingest failed")
		return
	}

	writeJSON(w, http.StatusOK, models.IngestResponse{
		Status:         "success",
		ChunksIngested: chunks,
	})
}

func isValidHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

func handleIngestSitemap(w http.ResponseWriter, r *http.Request) {
	var req models.IngestSitemapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.SitemapURL == "" {
		writeError(w, http.StatusBadRequest, "sitemap_url is required")
		return
	}
	if !isValidHTTPURL(req.SitemapURL) {
		writeError(w, http.StatusBadRequest, "sitemap_url must be an http(s) URL")
		return
	}

	if req.MaxConcurrent <= 0 {
		req.MaxConcurrent = 5
	}
	if req.MaxConcurrent > maxSitemapConcurrent {
		req.MaxConcurrent = maxSitemapConcurrent
	}

	jobID, err := jobs.CreateJob("crawl_sitemap", map[string]interface{}{
		"sitemap_url":    req.SitemapURL,
		"max_concurrent": req.MaxConcurrent,
	})
	if err != nil {
		logger.Error("Failed to create job: %s", err)
		writeError(w, http.StatusInternalServerError, "Failed to create ingestion job")
		return
	}

	job, err := jobs.GetJob(jobID)
	if err != nil || job == nil {
		logger.Error("Failed to get created job: %s", err)
		writeError(w, http.StatusInternalServerError, "Failed to get job")
		return
	}

	if WorkerPool == nil || !WorkerPool.Enqueue(job) {
		if uErr := jobs.UpdateJob(jobID, map[string]interface{}{"status": "failed"}); uErr != nil {
			logger.Error("Failed to mark rejected job %s failed: %s", jobID, uErr)
		}
		writeError(w, http.StatusServiceUnavailable, "Ingestion queue is full; try again later")
		return
	}

	logger.Info("Enqueued sitemap ingestion job %s", jobID)

	writeJSON(w, http.StatusOK, models.IngestSitemapAsyncResponse{
		JobID:  jobID,
		Status: "queued",
	})
}

func lookupJob(w http.ResponseWriter, jobID string) *jobs.Job {
	job, err := jobs.GetJob(jobID)
	if err != nil {
		if errors.Is(err, jobs.ErrNotConfigured) {
			writeError(w, http.StatusNotFound, "Job not found")
			return nil
		}
		logger.Error("Job store error for %s: %s", jobID, err)
		writeError(w, http.StatusServiceUnavailable, "Job store unavailable")
		return nil
	}
	if job == nil {
		writeError(w, http.StatusNotFound, "Job not found")
		return nil
	}
	return job
}

func handleIngestStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	job := lookupJob(w, jobID)
	if job == nil {
		return
	}

	total := job.ArticlesTotal
	done := job.ArticlesDone
	progressPct := 0.0
	if total > 0 {
		progressPct = math.Round(float64(done)/float64(total)*1000) / 10
	}

	writeJSON(w, http.StatusOK, models.JobStatusResponse{
		Status:       job.Status,
		ProgressPct:  progressPct,
		ArticlesDone: done,
		Total:        total,
		Errors:       job.Errors,
	})
}

func handleIngestResult(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	job := lookupJob(w, jobID)
	if job == nil {
		return
	}

	duration := 0.0
	if created, err := time.Parse(time.RFC3339, job.CreatedAt); err == nil {
		duration = math.Round(time.Since(created).Seconds()*10) / 10
	}

	writeJSON(w, http.StatusOK, models.JobResultResponse{
		Status:          job.Status,
		ChunksIngested:  job.ArticlesDone,
		DurationSeconds: duration,
		Errors:          job.Errors,
	})
}

func handleRetryDead(w http.ResponseWriter, r *http.Request) {
	var retriedJobIDs []string

	for {
		entries := jobs.PopDLQEntries(1)
		if len(entries) == 0 {
			break
		}
		entry := entries[0]

		jobID, err := jobs.CreateJob("crawl_sitemap", entry.Args)
		if err != nil {
			logger.Error("Failed to recreate DLQ job, returning entry to DLQ: %s", err)
			jobs.PushToDLQ(entry.JobID, entry.Task, entry.Args, entry.Error, entry.RetryCount)
			break
		}

		job, err := jobs.GetJob(jobID)
		if err != nil || job == nil {
			logger.Error("Failed to get recreated DLQ job: %s", err)
			continue
		}

		if WorkerPool != nil && WorkerPool.Enqueue(job) {
			retriedJobIDs = append(retriedJobIDs, jobID)
		} else {
			if uErr := jobs.UpdateJob(jobID, map[string]interface{}{"status": "failed"}); uErr != nil {
				logger.Error("Failed to mark DLQ job %s failed: %s", jobID, uErr)
			}
		}
	}

	logger.Info("Re-enqueued %d DLQ jobs", len(retriedJobIDs))

	writeJSON(w, http.StatusOK, models.RetryDeadResponse{
		RetriedCount: len(retriedJobIDs),
		JobIDs:       retriedJobIDs,
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	qdrantStatus := "ok"
	if err := qdrant.Health(ctx); err != nil {
		qdrantStatus = "unreachable"
	}

	redisStatus := "ok"
	if err := jobs.Health(ctx); err != nil {
		if errors.Is(err, jobs.ErrNotConfigured) {
			redisStatus = "not_configured"
		} else {
			redisStatus = "unreachable"
		}
	}

	modelsStatus := "unconfigured"
	if config.ModelsSpaceURL() != "" {
		modelsStatus = "configured"
	}

	status := "ok"
	if qdrantStatus != "ok" || redisStatus != "ok" {
		status = "degraded"
	}

	writeJSON(w, http.StatusOK, models.HealthResponse{
		Status: status,
		Qdrant: qdrantStatus,
		Redis:  redisStatus,
		Models: modelsStatus,
	})
}

func handleLinkGraph(w http.ResponseWriter, r *http.Request) {
	graph := rerank.GetLinkGraph()

	writeJSON(w, http.StatusOK, models.LinkGraphResponse{
		Status:    "success",
		URLCount:  len(graph),
		LinkGraph: graph,
	})
}
