package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axvilab/wsbro/adapter"
	"github.com/axvilab/wsbro/config"
	"github.com/axvilab/wsbro/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Logger()

	cfg := config.Load()
	srv := server.NewServer(cfg)
	srv.Version = Version

	dashboardData, _ := DashboardHTML.ReadFile("dashboard/index.html")
	srv.DashboardHTML = dashboardData

	switch cfg.AdapterDriver {
	case "redis":
		a, err := adapter.NewRedisAdapter(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to connect to Redis")
		}
		srv.SetAdapter(a)
	case "nats":
		a, err := adapter.NewNATSAdapter(cfg.NATSUrl)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to connect to NATS")
		}
		srv.SetAdapter(a)
	default:
		log.Info().Msg("Using memory adapter (single instance)")
	}

	srv.StartCleanup(5 * time.Minute)

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: srv.Router(),
	}

	go func() {
		log.Info().Str("port", cfg.Port).Msg("WSBro started")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = srv.Adapter.Close()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Shutdown error")
	}

	log.Info().Msg("Server stopped")
}
