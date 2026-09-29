// ----- chi router, 8 endpoints, CORS @ backend/internal/handlers/routes.go -----
package handlers

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
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
	"github.com/arnabwithab/AutoLinks/backend/internal/rerank"
	"github.com/arnabwithab/AutoLinks/backend/internal/search"
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
)

// WorkerPool is the shared worker pool instance, set by main.go.
var WorkerPool *jobs.WorkerPool

// NewRouter creates and configures the chi router with all endpoints.
func NewRouter(tokenVerifier auth.TokenVerifier) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(requestBodyLimiter(maxRequestBodyBytes))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   strings.Split(config.FrontendURL(), ","),
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

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

func handleRecommend(w http.ResponseWriter, r *http.Request) {
	var req models.RecommendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	if req.Alpha == 0 {
		req.Alpha = defaultAlpha
	}
	if req.MinSimilarity == 0 {
		req.MinSimilarity = defaultMinSimilarity
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

	trimmed := entities
	if len(trimmed) > 10 {
		trimmed = trimmed[:10]
	}

	var queries []string
	var validEntities []extract.Entity
	for _, e := range trimmed {
		if len(e.Text) > 50 {
			continue
		}
		q := e.Text + " - " + req.Text[:int(math.Min(float64(len(req.Text)), 200))]
		queries = append(queries, q)
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

			candidates, err := search.SearchSimilar(embedding, searchCandidateLimit, req.MinSimilarity)
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
	for r := range resultCh {
		allResults = append(allResults, r)
	}

	var recommendations []models.Recommendation
	selectedURLs := make(map[string]bool)

	for _, result := range allResults {
		reranked := rerank.RerankCandidates(result.candidates, req.Alpha, selectedURLs)

		for _, candidate := range reranked {
			if len(recommendations) >= maxRecommendations {
				break
			}

			contextSnippet := candidate.ChunkText
			if len(contextSnippet) > 150 {
				contextSnippet = contextSnippet[:150]
			}

			recommendations = append(recommendations, models.Recommendation{
				ExactPhrase:      result.entity.Text,
				ContextSnippet:   contextSnippet,
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

	if err := ingest.IngestArticle(req.URL, req.Content); err != nil {
		logger.Error("Ingest error: %s", err)
		writeError(w, http.StatusInternalServerError, "Ingest failed")
		return
	}

	writeJSON(w, http.StatusOK, models.IngestResponse{
		Status:         "success",
		ChunksIngested: 1,
	})
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

	if req.MaxConcurrent == 0 {
		req.MaxConcurrent = 5
	}

	urls := ingest.ParseSitemap(req.SitemapURL)
	if len(urls) == 0 {
		writeError(w, http.StatusBadRequest, "No URLs found in sitemap")
		return
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

	if WorkerPool != nil {
		WorkerPool.Enqueue(job)
	}

	logger.Info("Enqueued sitemap ingestion job %s (%d articles)", jobID, len(urls))

	writeJSON(w, http.StatusOK, models.IngestSitemapAsyncResponse{
		JobID:             jobID,
		Status:            "queued",
		EstimatedArticles: len(urls),
	})
}

func handleIngestStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")

	job, err := jobs.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}
	if job == nil {
		writeError(w, http.StatusNotFound, "Job not found")
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

	job, err := jobs.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}
	if job == nil {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}

	writeJSON(w, http.StatusOK, models.JobResultResponse{
		Status:          job.Status,
		ChunksIngested:  job.ArticlesDone,
		DurationSeconds: 0.0,
		Errors:          job.Errors,
	})
}

func handleRetryDead(w http.ResponseWriter, r *http.Request) {
	entries := jobs.PopDLQEntries(0)
	var retriedJobIDs []string

	for _, entry := range entries {
		jobID, err := jobs.CreateJob("crawl_sitemap", entry.Args)
		if err != nil {
			logger.Error("Failed to recreate DLQ job: %s", err)
			continue
		}

		job, err := jobs.GetJob(jobID)
		if err != nil || job == nil {
			logger.Error("Failed to get recreated DLQ job: %s", err)
			continue
		}

		if WorkerPool != nil {
			WorkerPool.Enqueue(job)
		}
		retriedJobIDs = append(retriedJobIDs, jobID)
	}

	logger.Info("Re-enqueued %d DLQ jobs", len(retriedJobIDs))

	writeJSON(w, http.StatusOK, models.RetryDeadResponse{
		RetriedCount: len(retriedJobIDs),
		JobIDs:       retriedJobIDs,
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	modelsAvailable := config.ModelsSpaceURL() != ""

	writeJSON(w, http.StatusOK, models.HealthResponse{
		Status:      "ok",
		ModelLoaded: modelsAvailable,
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
