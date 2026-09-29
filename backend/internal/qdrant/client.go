// ----- Qdrant client and collection management @ backend/internal/qdrant/client.go -----
package qdrant

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
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
			if parsedPort, convErr := strconv.Atoi(p); convErr == nil {
				port = parsedPort
			}
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
func EnsureCollection(vectorSize uint64) error {
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
		info, infoErr := c.GetCollectionInfo(ctx, collectionName)
		if infoErr != nil {
			return fmt.Errorf("failed to inspect collection %q: %w", collectionName, infoErr)
		}
		if size := info.GetConfig().GetParams().GetVectorsConfig().GetParams().GetSize(); size != 0 && size != vectorSize {
			return fmt.Errorf("collection %q has vector size %d, expected %d", collectionName, size, vectorSize)
		}
		return nil
	}

	createReq := &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     vectorSize,
			Distance: qdrant.Distance_Cosine,
		}),
	}

	if err := c.CreateCollection(ctx, createReq); err != nil {
		return fmt.Errorf("failed to create Qdrant collection: %w", err)
	}

	logger.Info("Created collection: %s", collectionName)
	return nil
}

// Health checks that the Qdrant server is reachable.
func Health(ctx context.Context) error {
	c, err := GetClient()
	if err != nil {
		return err
	}
	_, err = c.CollectionExists(ctx, config.QdrantCollection())
	return err
}

// DeletePointsByURL removes every point whose payload "url" matches articleURL.
// It is used to clear stale chunks before re-ingesting a page.
func DeletePointsByURL(ctx context.Context, articleURL string) error {
	c, err := GetClient()
	if err != nil {
		return err
	}

	filter := &qdrant.Filter{
		Must: []*qdrant.Condition{qdrant.NewMatchKeyword("url", articleURL)},
	}

	_, err = c.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: config.QdrantCollection(),
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{Filter: filter},
		},
	})
	if err != nil {
		return fmt.Errorf("qdrant delete failed: %w", err)
	}
	return nil
}
