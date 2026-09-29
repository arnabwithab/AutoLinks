// ----- inference client selection tests @ backend/internal/infer/client_test.go -----
package infer

import (
	"testing"
)

func TestNewFromConfigDefaultsToHF(t *testing.T) {
	t.Setenv("MODELS_BACKEND", "")
	t.Setenv("SAGEMAKER_ENDPOINT", "")
	c, err := NewFromConfig()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := c.(HFSpaceClient); !ok {
		t.Fatalf("expected HFSpaceClient, got %T", c)
	}
}

func TestNewFromConfigUnknownBackendFailsClosed(t *testing.T) {
	t.Setenv("MODELS_BACKEND", "bogus")
	if _, err := NewFromConfig(); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestNewFromConfigSageMakerNeedsEndpoint(t *testing.T) {
	t.Setenv("MODELS_BACKEND", "sagemaker")
	t.Setenv("SAGEMAKER_ENDPOINT", "")
	if _, err := NewFromConfig(); err == nil {
		t.Fatal("expected error when endpoint is unset")
	}
}
