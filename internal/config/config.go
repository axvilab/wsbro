package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// App holds credentials and per-app limits for a single Pusher-compatible application.
// Zero limits fall back to the server-wide values.
type App struct {
	ID                   string `json:"id"`
	Key                  string `json:"key"`
	Secret               string `json:"secret"`
	WebhookURL           string `json:"webhook_url"`
	MaxConnections       int    `json:"max_connections"`
	MaxMessagesPerSecond int    `json:"max_messages_per_second"`
	AllowedOrigins       string `json:"allowed_origins"`
}

// Config holds all server settings, populated from environment variables.
type Config struct {
	Host                 string
	Port                 string
	TLSCert              string
	TLSKey               string
	Apps                 []App
	AdapterDriver        string
	RedisAddr            string
	RedisPassword        string
	RedisDB              int
	NATSUrl              string
	DashboardPassword    string
	MaxMessagesPerSecond int
	WebhookURL           string
	MaxConnections       int
	AllowedOrigins       string
	MaxMessageSize       int
	MaxChannelsPerConn   int
	MaxEventDataSize     int
	LogLevel             string
	LogFormat            string
}

// Default credentials, used when nothing is configured. Fine for local development only.
const (
	DefaultAppID     = "app-id"
	DefaultAppKey    = "app-key"
	DefaultAppSecret = "app-secret"
)

// Load reads configuration from environment variables, supporting both single-app
// (WSBRO_APP_*) and multi-app (WSBRO_APPS JSON array) setups.
func Load() (*Config, error) {
	cfg := Default()
	cfg.Host = getEnv("WSBRO_HOST", cfg.Host)
	cfg.Port = getEnv("WSBRO_PORT", cfg.Port)
	cfg.TLSCert = getEnv("WSBRO_TLS_CERT", "")
	cfg.TLSKey = getEnv("WSBRO_TLS_KEY", "")
	cfg.AdapterDriver = getEnv("WSBRO_ADAPTER", cfg.AdapterDriver)
	cfg.RedisAddr = getEnv("WSBRO_REDIS_ADDR", cfg.RedisAddr)
	cfg.RedisPassword = getEnv("WSBRO_REDIS_PASSWORD", "")
	cfg.NATSUrl = getEnv("WSBRO_NATS_URL", cfg.NATSUrl)
	cfg.DashboardPassword = getEnv("WSBRO_DASHBOARD_PASSWORD", "")
	cfg.WebhookURL = getEnv("WSBRO_WEBHOOK_URL", "")
	cfg.AllowedOrigins = getEnv("WSBRO_ALLOWED_ORIGINS", cfg.AllowedOrigins)
	cfg.LogLevel = getEnv("WSBRO_LOG_LEVEL", cfg.LogLevel)
	cfg.LogFormat = getEnv("WSBRO_LOG_FORMAT", cfg.LogFormat)

	ints := []struct {
		key    string
		target *int
	}{
		{"WSBRO_REDIS_DB", &cfg.RedisDB},
		{"WSBRO_MAX_MESSAGES_PER_SECOND", &cfg.MaxMessagesPerSecond},
		{"WSBRO_MAX_CONNECTIONS", &cfg.MaxConnections},
		{"WSBRO_MAX_MESSAGE_SIZE", &cfg.MaxMessageSize},
		{"WSBRO_MAX_CHANNELS_PER_CONN", &cfg.MaxChannelsPerConn},
		{"WSBRO_MAX_EVENT_DATA_SIZE", &cfg.MaxEventDataSize},
	}
	for _, i := range ints {
		val := os.Getenv(i.key)
		if val == "" {
			continue
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("%s must be a number, got %q", i.key, val)
		}
		*i.target = n
	}

	if appsJSON := os.Getenv("WSBRO_APPS"); appsJSON != "" {
		var apps []App
		if err := json.Unmarshal([]byte(appsJSON), &apps); err != nil {
			return nil, fmt.Errorf("invalid WSBRO_APPS JSON: %w", err)
		}
		cfg.Apps = apps
	} else {
		cfg.Apps = []App{{
			ID:     getEnv("WSBRO_APP_ID", DefaultAppID),
			Key:    getEnv("WSBRO_APP_KEY", DefaultAppKey),
			Secret: getEnv("WSBRO_APP_SECRET", DefaultAppSecret),
		}}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Default returns a Config with sensible defaults and a single development app.
func Default() *Config {
	return &Config{
		Host: "0.0.0.0",
		Port: "6001",
		Apps: []App{{
			ID:     DefaultAppID,
			Key:    DefaultAppKey,
			Secret: DefaultAppSecret,
		}},
		AdapterDriver:        "memory",
		RedisAddr:            "localhost:6379",
		NATSUrl:              "nats://localhost:4222",
		MaxMessagesPerSecond: 100,
		AllowedOrigins:       "*",
		MaxMessageSize:       65536,
		MaxChannelsPerConn:   100,
		MaxEventDataSize:     10240,
		LogLevel:             "info",
		LogFormat:            "console",
	}
}

// Validate checks that the configuration is usable.
func (c *Config) Validate() error {
	if len(c.Apps) == 0 {
		return errors.New("at least one app must be configured")
	}

	ids := make(map[string]bool)
	keys := make(map[string]bool)
	for i, app := range c.Apps {
		if app.ID == "" || app.Key == "" || app.Secret == "" {
			return fmt.Errorf("app #%d: id, key and secret are required", i+1)
		}
		if ids[app.ID] {
			return fmt.Errorf("duplicate app id %q", app.ID)
		}
		if keys[app.Key] {
			return fmt.Errorf("duplicate app key %q", app.Key)
		}
		ids[app.ID] = true
		keys[app.Key] = true
	}

	switch c.AdapterDriver {
	case "memory", "redis", "nats":
	default:
		return fmt.Errorf("unknown adapter %q (use memory, redis or nats)", c.AdapterDriver)
	}

	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("WSBRO_TLS_CERT and WSBRO_TLS_KEY must be set together")
	}

	if c.MaxMessageSize <= 0 || c.MaxEventDataSize <= 0 {
		return errors.New("WSBRO_MAX_MESSAGE_SIZE and WSBRO_MAX_EVENT_DATA_SIZE must be positive")
	}
	return nil
}

// UsesDefaultSecret reports whether any app still runs with the built-in development secret.
func (c *Config) UsesDefaultSecret() bool {
	for _, app := range c.Apps {
		if app.Secret == DefaultAppSecret {
			return true
		}
	}
	return false
}

// FindAppByKey looks up an app by its public key (used in WebSocket and API auth).
func (c *Config) FindAppByKey(key string) *App {
	for i := range c.Apps {
		if c.Apps[i].Key == key {
			return &c.Apps[i]
		}
	}
	return nil
}

// FindAppByID looks up an app by its ID (used in REST API URL paths).
func (c *Config) FindAppByID(id string) *App {
	for i := range c.Apps {
		if c.Apps[i].ID == id {
			return &c.Apps[i]
		}
	}
	return nil
}

// OriginPatterns returns the host patterns allowed to open WebSockets for an app.
// Accepts both "example.com" and "https://example.com" forms. Empty means any origin.
func (c *Config) OriginPatterns(app *App) []string {
	raw := app.AllowedOrigins
	if raw == "" {
		raw = c.AllowedOrigins
	}
	if raw == "" || raw == "*" {
		return nil
	}

	var patterns []string
	for _, origin := range strings.Split(raw, ",") {
		origin = strings.TrimSpace(origin)
		origin = strings.TrimPrefix(origin, "https://")
		origin = strings.TrimPrefix(origin, "http://")
		origin = strings.TrimSuffix(origin, "/")
		if origin != "" {
			patterns = append(patterns, origin)
		}
	}
	return patterns
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
