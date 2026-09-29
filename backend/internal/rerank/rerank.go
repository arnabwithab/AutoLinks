// ----- equity-aware re-ranking and link graph @ backend/internal/rerank/rerank.go -----
package rerank

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/redis/go-redis/v9"
)

// LinkGraphKey is the legacy Redis key for the link graph (JSON blob, read-only fallback).
const LinkGraphKey = "autolinks:link_graph"

// LinkGraphHashKey is the Redis hash for the link graph (field per URL).
// Hash fields make multi-worker merges atomic per URL (§2); the JSON blob is
// only read once to migrate pre-distribution deployments.
const LinkGraphHashKey = "autolinks:link_graph:v2"

// GraphUpdateChannel carries "graph updated" invalidations (§3).
const GraphUpdateChannel = "autolinks:graph_updates"

// graphCacheTTL bounds how stale equity scores can get (§3: seconds-stale is fine).
const graphCacheTTL = time.Minute

var (
	linkGraph         map[string]int
	linkGraphMu       sync.RWMutex
	graphCacheExpires time.Time
	rerankRdb         *redis.Client
	rerankRdbOnce     sync.Once
)

func init() {
	linkGraph = make(map[string]int)
}

// InitLinkGraph initializes the link graph with pre-computed inbound link counts,
// replacing any existing graph.
func InitLinkGraph(graph map[string]int) {
	linkGraphMu.Lock()
	linkGraph = graph
	graphCacheExpires = time.Now().Add(graphCacheTTL)
	linkGraphMu.Unlock()
	replaceLinkGraph(graph)
	logger.Info("Link graph initialized with %d URLs", len(graph))
}

// MergeLinkGraph merges crawled inbound link counts into the existing graph.
// Per-URL last-writer-wins, so re-merging a crawl yields the same graph (§2);
// hash fields keep concurrent workers from clobbering each other.
func MergeLinkGraph(graph map[string]int) {
	linkGraphMu.Lock()
	for url, count := range graph {
		linkGraph[url] = count
	}
	total := len(linkGraph)
	graphCacheExpires = time.Now().Add(graphCacheTTL)
	linkGraphMu.Unlock()
	publishGraphFields(graph)
	logger.Info("Link graph merged: %d URLs total", total)
}

// RestoreLinkGraph restores the link graph from Redis on startup.
func RestoreLinkGraph() map[string]int {
	redisURL := config.RedisURL()
	if redisURL == "" {
		return map[string]int{}
	}

	if graph := loadLinkGraphHash(); len(graph) > 0 {
		linkGraphMu.Lock()
		linkGraph = graph
		graphCacheExpires = time.Now().Add(graphCacheTTL)
		linkGraphMu.Unlock()
		logger.Info("Link graph restored from Redis: %d URLs", len(graph))
		return graph
	}

	// One-time migration for pre-distribution deployments (JSON blob).
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
	graphCacheExpires = time.Now().Add(graphCacheTTL)
	linkGraphMu.Unlock()
	publishGraphFields(graph)

	logger.Info("Link graph restored from Redis: %d URLs", len(graph))
	return graph
}

// loadLinkGraphHash reads the graph hash; empty map when unset.
func loadLinkGraphHash() map[string]int {
	rdb := getRedisClient()
	ctx := context.Background()
	fields, err := rdb.HGetAll(ctx, LinkGraphHashKey).Result()
	if err != nil || len(fields) == 0 {
		return map[string]int{}
	}
	graph := make(map[string]int, len(fields))
	for url, raw := range fields {
		if n, err := strconv.Atoi(raw); err == nil {
			graph[url] = n
		}
	}
	return graph
}

// refreshGraphIfStale reloads the local cache when its TTL lapsed (§3).
// Seconds-stale scores are correct for every practical purpose.
func refreshGraphIfStale() {
	linkGraphMu.RLock()
	fresh := time.Now().Before(graphCacheExpires)
	linkGraphMu.RUnlock()
	if fresh || config.RedisURL() == "" {
		return
	}
	if graph := loadLinkGraphHash(); len(graph) > 0 {
		linkGraphMu.Lock()
		linkGraph = graph
		graphCacheExpires = time.Now().Add(graphCacheTTL)
		linkGraphMu.Unlock()
	}
}

// StartGraphSubscriber refreshes the local cache on "graph updated" messages.
// It returns when ctx ends; call once per process.
func StartGraphSubscriber(ctx context.Context) {
	if config.RedisURL() == "" {
		return
	}
	rdb := getRedisClient()
	sub := rdb.Subscribe(ctx, GraphUpdateChannel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			if graph := loadLinkGraphHash(); len(graph) > 0 {
				linkGraphMu.Lock()
				linkGraph = graph
				graphCacheExpires = time.Now().Add(graphCacheTTL)
				linkGraphMu.Unlock()
			}
		}
	}
}

// replaceLinkGraph swaps the persisted graph (Init semantics).
func replaceLinkGraph(graph map[string]int) {
	if config.RedisURL() == "" || len(graph) == 0 {
		return
	}
	rdb := getRedisClient()
	ctx := context.Background()
	pipe := rdb.Pipeline()
	pipe.Del(ctx, LinkGraphHashKey)
	for url, count := range graph {
		pipe.HSet(ctx, LinkGraphHashKey, url, count)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warning("Could not save link graph to Redis: %s", err)
		return
	}
	if err := rdb.Publish(ctx, GraphUpdateChannel, "updated").Err(); err != nil {
		logger.Warning("Could not publish graph update: %s", err)
	}
}

// publishGraphFields persists merged fields and notifies other tasks.
func publishGraphFields(graph map[string]int) {
	if config.RedisURL() == "" || len(graph) == 0 {
		return
	}
	rdb := getRedisClient()
	ctx := context.Background()
	pipe := rdb.Pipeline()
	for url, count := range graph {
		pipe.HSet(ctx, LinkGraphHashKey, url, count)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warning("Could not save link graph to Redis: %s", err)
		return
	}
	if err := rdb.Publish(ctx, GraphUpdateChannel, "updated").Err(); err != nil {
		logger.Warning("Could not publish graph update: %s", err)
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

	result := make([]Candidate, 0, len(bestByURL))
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
	refreshGraphIfStale()
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
