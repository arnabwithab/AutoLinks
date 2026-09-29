// ----- HTTP server entry point with goroutine worker pool @ backend/cmd/server/main.go -----
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/boot"
	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/handlers"
	"github.com/arnabwithab/AutoLinks/backend/internal/jobs"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
)

func main() {
	logger.Info("Starting %s", config.AppName())

	boot.EnsureQdrant()

	streamCtx, stopStreams := context.WithCancel(context.Background())
	defer stopStreams()
	boot.InitGraph(streamCtx)

	pool := jobs.NewWorkerPool()
	if n := jobs.ReconcileJobs(pool); n > 0 {
		logger.Info("Re-enqueued %d pending jobs from Redis", n)
	}

	// Distributed slow path (§2): stream consumers share work across replicas.
	// The in-process pool stays for local fallback; the API enqueues to the stream.
	streamWG := pool.RunStreamConsumers(streamCtx, 4)

	tokenVerifier := boot.AuthVerifier()
	boot.LogFlags()

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

	stopStreams()
	waitStreams(streamWG)
	pool.Stop()

	logger.Info("Server stopped")
}

// waitStreams drains in-flight stream jobs with a timeout.
func waitStreams(wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		logger.Warning("Stream drain timed out, exiting anyway")
	}
}
