// ----- request/response structs with JSON tags @ backend/internal/models/models.go -----
package models

// RecommendRequest is the request body for /recommend.
// Alpha and MinSimilarity are pointers so an explicit 0 is distinguishable
// from an omitted field.
type RecommendRequest struct {
	Text          string   `json:"text"`
	Alpha         *float64 `json:"alpha"`
	MinSimilarity *float64 `json:"min_similarity"`
}

// IngestRequest is the request body for /ingest.
type IngestRequest struct {
	URL     string `json:"url"`
	Content string `json:"content"`
}

// IngestSitemapRequest is the request body for /ingest/sitemap.
type IngestSitemapRequest struct {
	SitemapURL    string `json:"sitemap_url"`
	MaxConcurrent int    `json:"max_concurrent"`
}

// Recommendation is a single link recommendation.
type Recommendation struct {
	ExactPhrase      string  `json:"exact_phrase"`
	ContextSnippet   string  `json:"context_snippet"`
	SuggestedURL     string  `json:"suggested_url"`
	SimilarityScore  float64 `json:"similarity_score"`
	EquityNeedScore  float64 `json:"equity_need_score"`
	FinalScore       float64 `json:"final_score"`
	InboundLinkCount int     `json:"inbound_link_count"`
}

// RecommendResponse is the response for /recommend.
type RecommendResponse struct {
	Status          string           `json:"status"`
	LatencyMs       int64            `json:"latency_ms"`
	Recommendations []Recommendation `json:"recommendations"`
}

// IngestResponse is the response for /ingest.
type IngestResponse struct {
	Status         string `json:"status"`
	ChunksIngested int    `json:"chunks_ingested"`
}

// IngestSitemapAsyncResponse is the response for async /ingest/sitemap.
type IngestSitemapAsyncResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// JobStatusResponse is the response for /ingest/status/{job_id}.
type JobStatusResponse struct {
	Status       string   `json:"status"`
	ProgressPct  float64  `json:"progress_pct"`
	ArticlesDone int      `json:"articles_done"`
	Total        int      `json:"total"`
	Errors       []string `json:"errors"`
}

// JobResultResponse is the response for /ingest/result/{job_id}.
type JobResultResponse struct {
	Status          string   `json:"status"`
	ChunksIngested  int      `json:"chunks_ingested"`
	DurationSeconds float64  `json:"duration_seconds"`
	Errors          []string `json:"errors"`
}

// RetryDeadResponse is the response for /ingest/retry-dead.
type RetryDeadResponse struct {
	RetriedCount int      `json:"retried_count"`
	JobIDs       []string `json:"job_ids"`
}

// HealthResponse is the response for /health.
type HealthResponse struct {
	Status string `json:"status"`
	Qdrant string `json:"qdrant"`
	Redis  string `json:"redis"`
	Models string `json:"models"`
}

// LinkGraphResponse is the response for /link-graph.
type LinkGraphResponse struct {
	Status    string         `json:"status"`
	URLCount  int            `json:"url_count"`
	LinkGraph map[string]int `json:"link_graph"`
}
