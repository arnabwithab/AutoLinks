// ----- equity-aware re-ranking and link graph @ backend/internal/rerank/rerank.go -----
package rerank

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"sync"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/redis/go-redis/v9"
)

// LinkGraphKey is the Redis key for the link graph.
const LinkGraphKey = "autolinks:link_graph"

var (
	linkGraph     map[string]int
	linkGraphMu   sync.RWMutex
	rerankRdb     *redis.Client
	rerankRdbErr  error
	rerankRdbOnce sync.Once
)

func init() {
	linkGraph = make(map[string]int)
}

// InitLinkGraph initializes the link graph with pre-computed inbound link counts,
// replacing any existing graph.
func InitLinkGraph(graph map[string]int) {
	linkGraphMu.Lock()
	defer linkGraphMu.Unlock()
	linkGraph = graph
	saveLinkGraph(graph)
	logger.Info("Link graph initialized with %d URLs", len(linkGraph))
}

// MergeLinkGraph merges crawled inbound link counts into the existing graph.
// URLs already present are updated; URLs from other crawls are preserved, so
// crawling a second sitemap does not wipe the first one's equity data.
func MergeLinkGraph(graph map[string]int) {
	linkGraphMu.Lock()
	defer linkGraphMu.Unlock()
	for url, count := range graph {
		linkGraph[url] = count
	}
	saveLinkGraph(linkGraph)
	logger.Info("Link graph merged: %d URLs total", len(linkGraph))
}

// RestoreLinkGraph restores the link graph from Redis on startup.
func RestoreLinkGraph() map[string]int {
	redisURL := config.RedisURL()
	if redisURL == "" {
		return map[string]int{}
	}

	rdb := getRedisClient()
	ctx := context.Background()
	raw, err := rdb.Get(ctx, LinkGraphKey).Result()
	if err != nil {
		logger.Warning("Could not restore link graph from Redis: %s", err)
		return map[string]int{}
	}

	var graph map[string]int
	if err := json.Unmarshal([]byte(raw), &graph); err != nil {
		logger.Warning("Could not unmarshal link graph from Redis: %s", err)
		return map[string]int{}
	}

	linkGraphMu.Lock()
	linkGraph = graph
	linkGraphMu.Unlock()

	logger.Info("Link graph restored from Redis: %d URLs", len(graph))
	return graph
}

func saveLinkGraph(graph map[string]int) {
	redisURL := config.RedisURL()
	if redisURL == "" || len(graph) == 0 {
		return
	}

	rdb := getRedisClient()
	data, err := json.Marshal(graph)
	if err != nil {
		logger.Warning("Could not save link graph to Redis: %s", err)
		return
	}

	ctx := context.Background()
	if err := rdb.Set(ctx, LinkGraphKey, data, 0).Err(); err != nil {
		logger.Warning("Could not save link graph to Redis: %s", err)
	}
}

func getRedisClient() *redis.Client {
	rerankRdbOnce.Do(func() {
		redisURL := config.RedisURL()
		if redisURL == "" {
			return
		}
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			logger.Error("Failed to parse Redis URL: %s", err)
			rerankRdbErr = err
			return
		}
		rerankRdb = redis.NewClient(opts)
	})
	return rerankRdb
}

// EquityNeed calculates equity need score for a URL (higher = more need).
func EquityNeed(inboundLinks int) float64 {
	return 1.0 / (1.0 + float64(inboundLinks))
}

// FinalScore computes final combined score using similarity + equity need.
func FinalScore(similarity float64, inboundLinks int, alpha float64) float64 {
	eqNeed := EquityNeed(inboundLinks)
	return alpha*similarity + (1-alpha)*eqNeed
}

// CollapseCandidatesByURL collapses chunk-level search hits into one best candidate per URL.
func CollapseCandidatesByURL(candidates []Candidate) []Candidate {
	bestByURL := make(map[string]Candidate)

	for _, candidate := range candidates {
		url := candidate.URL
		if url == "" {
			continue
		}

		existing, ok := bestByURL[url]
		if !ok || candidate.Score > existing.Score {
			bestByURL[url] = candidate
		}
	}

	var result []Candidate
	for _, c := range bestByURL {
		result = append(result, c)
	}
	return result
}

// Candidate represents a raw Qdrant search hit enriched with equity scores.
type Candidate struct {
	URL              string
	ChunkText        string
	Score            float64
	InboundLinkCount int
	EquityNeedScore  float64
	FinalScore       float64
}

// RerankCandidates re-ranks Qdrant results using equity-aware scoring.
// alpha must be resolved by the caller (0 is a valid, pure-equity value).
func RerankCandidates(candidates []Candidate, alpha float64, excludedURLs map[string]bool) []Candidate {
	uniqueCandidates := CollapseCandidatesByURL(candidates)

	var reranked []Candidate
	for _, candidate := range uniqueCandidates {
		if excludedURLs != nil && excludedURLs[candidate.URL] {
			continue
		}

		linkGraphMu.RLock()
		inboundCount := linkGraph[candidate.URL]
		linkGraphMu.RUnlock()

		eqNeed := EquityNeed(inboundCount)
		final := FinalScore(candidate.Score, inboundCount, alpha)

		reranked = append(reranked, Candidate{
			URL:              candidate.URL,
			ChunkText:        candidate.ChunkText,
			Score:            candidate.Score,
			InboundLinkCount: inboundCount,
			EquityNeedScore:  math.Round(eqNeed*10000) / 10000,
			FinalScore:       math.Round(final*10000) / 10000,
		})
	}

	sort.Slice(reranked, func(i, j int) bool {
		return reranked[i].FinalScore > reranked[j].FinalScore
	})

	logger.Info("Re-ranked %d unique URL candidates from %d raw chunks", len(reranked), len(candidates))
	return reranked
}

// GetLinkGraph returns a copy of the current link graph.
func GetLinkGraph() map[string]int {
	linkGraphMu.RLock()
	defer linkGraphMu.RUnlock()
	result := make(map[string]int, len(linkGraph))
	for k, v := range linkGraph {
		result[k] = v
	}
	return result
}
