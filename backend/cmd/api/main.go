// ----- API-only entry point (fast path) @ backend/cmd/api/main.go -----
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/boot"
	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/handlers"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
)

func main() {
	logger.Info("Starting %s api", config.AppName())

	boot.EnsureQdrant()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	boot.InitGraph(ctx)

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
		logger.Info("API listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("API failed: %s", err)
		}
	}()

	sig := <-shutdown
	logger.Info("Received %s, shutting down gracefully...", sig)

	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Error("API forced to shutdown: %s", err)
	}

	logger.Info("API stopped")
}
