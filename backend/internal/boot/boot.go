// ----- shared service boot helpers @ backend/internal/boot/boot.go -----
package boot

import (
	"context"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/auth"
	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/arnabwithab/AutoLinks/backend/internal/qdrant"
	"github.com/arnabwithab/AutoLinks/backend/internal/rerank"
	"github.com/clerkinc/clerk-sdk-go/clerk"
)

// EnsureQdrant retries collection setup, failing closed when Qdrant stays down.
func EnsureQdrant() {
	var qErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if qErr = qdrant.EnsureCollection(384); qErr == nil {
			return
		}
		logger.Warning("Qdrant not ready (attempt %d/3): %s", attempt, qErr)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	logger.Fatal("Failed to ensure Qdrant collection: %s", qErr)
}

// InitGraph restores the link graph and refreshes it on pub/sub updates.
func InitGraph(ctx context.Context) {
	rerank.RestoreLinkGraph()
	go rerank.StartGraphSubscriber(ctx)
}

// AuthVerifier builds the Clerk verifier, honoring the explicit auth-off hatch.
func AuthVerifier() auth.TokenVerifier {
	switch {
	case config.ClerkSecretKey() != "":
		cl, err := clerk.NewClient(config.ClerkSecretKey())
		if err != nil {
			logger.Fatal("Failed to create Clerk client: %s", err)
		}
		logger.Info("Clerk auth enabled")
		return cl
	case config.AuthDisabled():
		logger.Warning("AUTH_DISABLED=true — auth is OFF; all endpoints are publicly accessible")
		return nil
	default:
		logger.Fatal("CLERK_SECRET_KEY is not set; set it to enable auth, or set AUTH_DISABLED=true to explicitly run without auth")
		return nil
	}
}

// LogFlags reports debug/dry-run mode at startup.
func LogFlags() {
	if config.Debug() {
		logger.Info("Debug mode enabled")
	}
	if config.DryRun() {
		logger.Warning("DRY_RUN enabled - using fixture data")
	}
}
