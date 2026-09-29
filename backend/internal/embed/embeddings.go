// ----- embedding generation via HF Space @ backend/internal/embed/embeddings.go -----
package embed

import (
	"encoding/json"
	"fmt"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/extract"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
)

// EmbedText generates an embedding vector for a single text.
func EmbedText(text string) ([]float64, error) {
	embeddings, err := EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for a batch of texts via HF Space.
func EmbedBatch(texts []string) ([][]float64, error) {
	if config.ModelsSpaceURL() == "" {
		return nil, fmt.Errorf("models_space_url not configured")
	}

	textsJSON, err := json.Marshal(texts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal texts: %w", err)
	}

	result, err := extract.CallSpace("embed_text", string(textsJSON))
	if err != nil {
		return nil, fmt.Errorf("embed_text failed: %w", err)
	}

	var rawJSON string
	if err := json.Unmarshal([]byte(result), &rawJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal embedding wrapper: %w", err)
	}

	var embeddings [][]float64
	if err := json.Unmarshal([]byte(rawJSON), &embeddings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal embeddings: %w", err)
	}

	logger.Info("Generated %d embeddings via HF Space", len(embeddings))
	return embeddings, nil
}
