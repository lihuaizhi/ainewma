// Command server exposes the LLM client over HTTP.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/httpapi"
	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm/openai"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client, err := openai.New(openai.Options{
		APIURL:                cfg.LLMAPIURL,
		APIKey:                cfg.APIKey,
		DefaultModel:          cfg.Model,
		OrgID:                 cfg.OrgID,
		ProjectID:             cfg.ProjectID,
		MaxRetries:            cfg.MaxRetries,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: httpapi.NewHandler(client, cfg.Model, cfg.MaxRequestBody),
		// ReadHeaderTimeout guards the header phase only. WriteTimeout stays
		// zero because a streaming response legitimately runs for minutes.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("server started", "addr", cfg.HTTPAddr, "upstream", cfg.LLMAPIURL, "model", cfg.Model)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	select {
	case err := <-serverErrors:
		return err
	case <-shutdownContext.Done():
	}

	slog.Info("shutting down")
	gracefulContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(gracefulContext); err != nil {
		return err
	}
	return <-serverErrors
}
