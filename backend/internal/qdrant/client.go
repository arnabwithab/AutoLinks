// ----- Qdrant client and collection management @ backend/internal/qdrant/client.go -----
package qdrant

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	qdrant "github.com/qdrant/go-client/qdrant"
)

var (
	client     *qdrant.Client
	clientOnce sync.Once
	initErr    error
)

// GetClient returns the initialized Qdrant client.
func GetClient() (*qdrant.Client, error) {
	clientOnce.Do(func() {
		qdrantURL := config.QdrantURL()
		apiKey := config.QdrantAPIKey()

		parsed, err := url.Parse(qdrantURL)
		if err != nil {
			initErr = fmt.Errorf("invalid QDRANT_URL: %w", err)
			return
		}

		host := parsed.Hostname()
		port := 6334
		if p := parsed.Port(); p != "" {
			fmt.Sscanf(p, "%d", &port)
		}

		useTLS := parsed.Scheme == "https" || strings.Contains(host, "cloud.qdrant.io")

		cfg := &qdrant.Config{
			Host:   host,
			Port:   port,
			APIKey: apiKey,
			UseTLS: useTLS,
		}

		c, err := qdrant.NewClient(cfg)
		if err != nil {
			initErr = fmt.Errorf("failed to create Qdrant client: %w", err)
			return
		}
		client = c
		logger.Info("Qdrant client initialized")
	})
	return client, initErr
}

// EnsureCollection creates the articles collection if it doesn't exist.
func EnsureCollection(vectorSize int) error {
	c, err := GetClient()
	if err != nil {
		return err
	}

	collectionName := config.QdrantCollection()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := c.CollectionExists(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("failed to check collection existence: %w", err)
	}
	if exists {
		return nil
	}

	createReq := &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(vectorSize),
			Distance: qdrant.Distance_Cosine,
		}),
	}

	if err := c.CreateCollection(ctx, createReq); err != nil {
		return fmt.Errorf("failed to create Qdrant collection: %w", err)
	}

	logger.Info("Created collection: %s", collectionName)
	return nil
}

// SearchPerformer returns the Qdrant client for performing searches.
func SearchPerformer() (*qdrant.Client, error) {
	return GetClient()
}
