// ----- worker-only entry point (slow path) @ backend/cmd/worker/main.go -----
package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/boot"
	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/jobs"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
)

func main() {
	logger.Info("Starting %s worker", config.AppName())

	boot.EnsureQdrant()
	boot.LogFlags()

	pool := jobs.NewWorkerPool()
	if n := jobs.ReconcileJobs(pool); n > 0 {
		logger.Info("Re-enqueued %d pending jobs from Redis", n)
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	var streamWG *sync.WaitGroup
	if err := jobs.EnsureStreamGroup(ctx); err != nil {
		logger.Fatal("Stream group unavailable: %s", err)
	} else {
		streamWG = pool.RunStreamConsumers(ctx, 4)
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdown
	logger.Info("Received %s, draining in-flight crawls...", sig)

	stop()
	if streamWG != nil {
		done := make(chan struct{})
		go func() {
			streamWG.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Minute):
			logger.Warning("Drain timed out, exiting anyway (jobs will be reclaimed)")
		}
	}
	pool.Stop()

	logger.Info("Worker stopped")
}
