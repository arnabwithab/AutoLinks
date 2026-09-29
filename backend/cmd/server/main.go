// ----- HTTP server entry point with goroutine worker pool @ backend/cmd/server/main.go -----
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/auth"
	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/handlers"
	"github.com/arnabwithab/AutoLinks/backend/internal/jobs"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/arnabwithab/AutoLinks/backend/internal/qdrant"
	"github.com/arnabwithab/AutoLinks/backend/internal/rerank"
	"github.com/clerkinc/clerk-sdk-go/clerk"
)

func main() {
	logger.Info("Starting %s", config.AppName())

	var qErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if qErr = qdrant.EnsureCollection(384); qErr == nil {
			break
		}
		logger.Warning("Qdrant not ready (attempt %d/3): %s", attempt, qErr)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if qErr != nil {
		logger.Fatal("Failed to ensure Qdrant collection: %s", qErr)
	}

	rerank.RestoreLinkGraph()

	handlers.WorkerPool = jobs.NewWorkerPool()
	if n := jobs.ReconcileJobs(handlers.WorkerPool); n > 0 {
		logger.Info("Re-enqueued %d pending jobs from Redis", n)
	}

	// Distributed slow path (§2): stream consumers share work across replicas.
	// The in-process pool stays for local fallback; the API enqueues to the stream.
	streamCtx, stopStreams := context.WithCancel(context.Background())
	defer stopStreams()
	if err := jobs.EnsureStreamGroup(streamCtx); err != nil {
		logger.Warning("Stream group unavailable, ingest will 503: %s", err)
	} else {
		handlers.WorkerPool.RunStreamConsumers(streamCtx, 4)
	}
	go rerank.StartGraphSubscriber(streamCtx)

	var tokenVerifier auth.TokenVerifier
	switch {
	case config.ClerkSecretKey() != "":
		cl, err := clerk.NewClient(config.ClerkSecretKey())
		if err != nil {
			logger.Fatal("Failed to create Clerk client: %s", err)
		}
		tokenVerifier = cl
		logger.Info("Clerk auth enabled")
	case config.AuthDisabled():
		logger.Warning("AUTH_DISABLED=true — auth is OFF; all endpoints are publicly accessible")
	default:
		logger.Fatal("CLERK_SECRET_KEY is not set; set it to enable auth, or set AUTH_DISABLED=true to explicitly run without auth")
	}

	if config.Debug() {
		logger.Info("Debug mode enabled")
	}
	if config.DryRun() {
		logger.Warning("DRY_RUN enabled - using fixture data")
	}

	router := handlers.NewRouter(tokenVerifier)

	port := config.Port()
	addr := fmt.Sprintf(":%s", port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("Server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed: %s", err)
		}
	}()

	sig := <-shutdown
	logger.Info("Received %s, shutting down gracefully...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown: %s", err)
	}

	handlers.WorkerPool.Stop()

	logger.Info("Server stopped")
}
