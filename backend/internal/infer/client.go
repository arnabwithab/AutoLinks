// ----- inference backend abstraction (HF Space, SageMaker) @ backend/internal/infer/client.go -----
package infer

import (
	"context"
	"fmt"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/embed"
	"github.com/arnabwithab/AutoLinks/backend/internal/extract"
)

// Entity is an alias so callers do not need to import extract for types.
type Entity = extract.Entity

// Client abstracts NER + embedding inference behind a swappable backend.
// HF Space is the current backend; SageMaker is provisioned in parallel
// and selected via MODELS_BACKEND=sagemaker once warm.
type Client interface {
	ExtractEntities(ctx context.Context, text string) ([]Entity, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
}

// HFSpaceClient delegates to the existing Gradio Space helpers.
type HFSpaceClient struct{}

// ExtractEntities runs GLiNER via the HF Space.
func (HFSpaceClient) ExtractEntities(_ context.Context, text string) ([]Entity, error) {
	return extract.ExtractEntities(text)
}

// EmbedBatch runs MiniLM via the HF Space.
func (HFSpaceClient) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	return embed.EmbedBatch(texts)
}

// SageMakerClient invokes the shared SageMaker endpoint.
// It is a stub until the endpoint is provisioned; it fails closed so a
// misconfigured MODELS_BACKEND never silently falls back to another backend.
type SageMakerClient struct {
	endpoint string
}

// ExtractEntities is not yet implemented for SageMaker.
func (c SageMakerClient) ExtractEntities(_ context.Context, _ string) ([]Entity, error) {
	return nil, fmt.Errorf("sagemaker endpoint %q: entity inference not wired yet", c.endpoint)
}

// EmbedBatch is not yet implemented for SageMaker.
func (c SageMakerClient) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return nil, fmt.Errorf("sagemaker endpoint %q: embed inference not wired yet", c.endpoint)
}

// NewFromConfig returns the inference client selected by MODELS_BACKEND.
// Unknown values fail closed; "" defaults to HF Space (current behavior).
func NewFromConfig() (Client, error) {
	backend := config.ModelsBackend()
	endpoint := config.SageMakerEndpoint()
	switch backend {
	case "", "hf", "hf_space":
		return HFSpaceClient{}, nil
	case "sagemaker":
		if endpoint == "" {
			return nil, fmt.Errorf("MODELS_BACKEND=sagemaker but SAGEMAKER_ENDPOINT is unset")
		}
		return SageMakerClient{endpoint: endpoint}, nil
	default:
		return nil, fmt.Errorf("unknown MODELS_BACKEND %q (want hf or sagemaker)", backend)
	}
}
