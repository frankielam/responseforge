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

	"responseforge/internal/config"
	"responseforge/internal/httpapi"
	"responseforge/internal/ingest"
	"responseforge/internal/llm"
	"responseforge/internal/storage"
)

func main() {
	cfg, err := config.Load()
	level := slog.LevelInfo
	if err == nil && cfg.Debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	if err != nil {
		log.Error("config", "error", err.Error())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var store storage.Store
	switch cfg.Storage {
	case "postgres":
		pg, err := storage.OpenPostgres(ctx, cfg.DatabaseURL, cfg.PostgresTimeout)
		if err != nil {
			log.Error("postgres", "error", err.Error())
			os.Exit(1)
		}
		defer pg.Close()
		store = pg
	default:
		csvStore, err := storage.OpenCSV(cfg.DataDir)
		if err != nil {
			log.Error("csv", "error", err.Error())
			os.Exit(1)
		}
		store = csvStore
	}

	var model llm.Client
	if cfg.LLMEnabled() {
		model = llm.NewHTTP(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTimeout)
	}
	svc := ingest.New(cfg, store, model, log)
	api := httpapi.New(cfg, svc)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Info("listening", "addr", cfg.Addr, "storage", cfg.Storage, "llm", cfg.LLMEnabled())
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "error", err.Error())
		os.Exit(1)
	}
}
