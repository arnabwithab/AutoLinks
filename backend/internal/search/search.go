// ----- vector search via Qdrant @ backend/internal/search/search.go -----
package search

import (
	"context"
	"fmt"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/arnabwithab/AutoLinks/backend/internal/qdrant"
	qdrantpb "github.com/qdrant/go-client/qdrant"
)

// SearchResult represents a single Qdrant search hit.
type SearchResult struct {
	URL       string  `json:"url"`
	ChunkText string  `json:"chunk_text"`
	Score     float64 `json:"score"`
}

// SearchSimilar searches Qdrant for semantically similar article chunks.
func SearchSimilar(queryEmbedding []float64, limit int, minScore float64) ([]SearchResult, error) {
	client, err := qdrant.SearchPerformer()
	if err != nil {
		return nil, fmt.Errorf("failed to get qdrant client: %w", err)
	}

	collectionName := config.QdrantCollection()

	vector32 := make([]float32, len(queryEmbedding))
	for i, v := range queryEmbedding {
		vector32[i] = float32(v)
	}

	limit64 := uint64(limit)
	scoreThreshold := float32(minScore)

	req := &qdrantpb.QueryPoints{
		CollectionName: collectionName,
		Query:          qdrantpb.NewQuery(vector32...),
		Limit:          &limit64,
		ScoreThreshold: &scoreThreshold,
		WithPayload:    qdrantpb.NewWithPayload(true),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Query(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("qdrant search failed: %w", err)
	}

	var results []SearchResult
	for _, point := range resp {
		payload := point.GetPayload()
		if payload == nil {
			continue
		}
		urlVal := payload["url"]
		chunkVal := payload["chunk_text"]
		if urlVal == nil || chunkVal == nil {
			continue
		}
		results = append(results, SearchResult{
			URL:       urlVal.GetStringValue(),
			ChunkText: chunkVal.GetStringValue(),
			Score:     float64(point.GetScore()),
		})
	}

	logger.Info("Qdrant search returned %d results", len(results))
	return results, nil
}
