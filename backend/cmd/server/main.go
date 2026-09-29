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

	if err := qdrant.EnsureCollection(384); err != nil {
		logger.Fatal("Failed to ensure Qdrant collection: %s", err)
	}

	rerank.RestoreLinkGraph()

	handlers.WorkerPool = jobs.NewWorkerPool()

	var tokenVerifier auth.TokenVerifier
	if sk := config.ClerkSecretKey(); sk != "" {
		cl, err := clerk.NewClient(sk)
		if err != nil {
			logger.Fatal("Failed to create Clerk client: %s", err)
		}
		tokenVerifier = cl
		logger.Info("Clerk auth enabled")
	} else {
		logger.Warning("CLERK_SECRET_KEY not set — auth disabled")
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

	logger.Info("Server stopped")
}
