package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axvilab/wsbro/internal/adapter"
	"github.com/axvilab/wsbro/internal/config"
	"github.com/axvilab/wsbro/internal/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Version is set at build time: go build -ldflags="-X main.Version=v1.2.3".
var Version = "dev"

//go:embed dashboard
var dashboardFS embed.FS

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("wsbro", Version)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wsbro: "+err.Error())
		os.Exit(1)
	}
	setupLogger(cfg)

	if err := run(cfg); err != nil {
		log.Fatal().Err(err).Msg("WSBro stopped")
	}
}

func run(cfg *config.Config) error {
	srv := server.NewServer(cfg)
	srv.Version = Version
	srv.Dashboard, _ = fs.Sub(dashboardFS, "dashboard")

	relay, err := newAdapter(cfg)
	if err != nil {
		return fmt.Errorf("%s adapter: %w", cfg.AdapterDriver, err)
	}
	if relay != nil {
		if err := srv.SetAdapter(relay); err != nil {
			return fmt.Errorf("%s adapter: %w", cfg.AdapterDriver, err)
		}
	}

	if cfg.UsesDefaultSecret() {
		log.Warn().Msg("Running with the default app secret. Set WSBRO_APP_SECRET before going to production.")
	}
	if cfg.DashboardPassword == "" {
		log.Info().Msg("Dashboard is disabled. Set WSBRO_DASHBOARD_PASSWORD to enable it.")
	}

	httpServer := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info().
			Str("version", Version).
			Str("addr", httpServer.Addr).
			Str("adapter", srv.AdapterName()).
			Int("apps", len(cfg.Apps)).
			Bool("tls", cfg.TLSCert != "").
			Msg("WSBro started")
		if cfg.TLSCert != "" {
			errs <- httpServer.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
			return
		}
		errs <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errs:
		return err
	case <-stop:
	}

	log.Info().Msg("Shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv.Shutdown()
	if err := httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info().Msg("WSBro stopped")
	return nil
}

func newAdapter(cfg *config.Config) (adapter.Adapter, error) {
	switch cfg.AdapterDriver {
	case "redis":
		return adapter.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	case "nats":
		return adapter.NewNATS(cfg.NATSUrl)
	default:
		return nil, nil
	}
}

func setupLogger(cfg *config.Config) {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil || level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.LogFormat == "json" {
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
		return
	}
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Logger()
}
