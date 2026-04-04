package config

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/rs/zerolog/log"
)

// App holds credentials and per-app limits for a single Pusher-compatible application.
type App struct {
	ID                   string `json:"id"`
	Key                  string `json:"key"`
	Secret               string `json:"secret"`
	WebhookURL           string `json:"webhook_url"`
	MaxConnections       int    `json:"max_connections"`
	MaxMessagesPerSecond int    `json:"max_messages_per_second"`
	AllowedOrigins       string `json:"allowed_origins"`
}

// Config holds all server-level settings, populated from environment variables.
type Config struct {
	Port                 string
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
}

// Load reads configuration from environment variables, supporting both single-app
// (WSBRO_APP_*) and multi-app (WSBRO_APPS JSON array) setups.
func Load() *Config {
	cfg := &Config{
		Port:                 getEnv("WSBRO_PORT", "6001"),
		AdapterDriver:        getEnv("WSBRO_ADAPTER", "memory"),
		RedisAddr:            getEnv("WSBRO_REDIS_ADDR", "localhost:6379"),
		RedisPassword:        getEnv("WSBRO_REDIS_PASSWORD", ""),
		RedisDB:              0,
		NATSUrl:              getEnv("WSBRO_NATS_URL", "nats://localhost:4222"),
		DashboardPassword:    getEnv("WSBRO_DASHBOARD_PASSWORD", ""),
		MaxMessagesPerSecond: getEnvInt("WSBRO_MAX_MESSAGES_PER_SECOND", 100),
		MaxConnections:       getEnvInt("WSBRO_MAX_CONNECTIONS", 0),
		AllowedOrigins:       getEnv("WSBRO_ALLOWED_ORIGINS", "*"),
		MaxMessageSize:       getEnvInt("WSBRO_MAX_MESSAGE_SIZE", 65536),
		MaxChannelsPerConn:   getEnvInt("WSBRO_MAX_CHANNELS_PER_CONN", 100),
		MaxEventDataSize:     getEnvInt("WSBRO_MAX_EVENT_DATA_SIZE", 10240),
		WebhookURL:           getEnv("WSBRO_WEBHOOK_URL", ""),
	}

	if appsJSON := os.Getenv("WSBRO_APPS"); appsJSON != "" {
		var apps []App
		if err := json.Unmarshal([]byte(appsJSON), &apps); err != nil {
			log.Fatal().Err(err).Msg("Invalid WSBRO_APPS JSON")
		}
		cfg.Apps = apps
	} else {
		cfg.Apps = []App{
			{
				ID:     getEnv("WSBRO_APP_ID", "app-id"),
				Key:    getEnv("WSBRO_APP_KEY", "app-key"),
				Secret: getEnv("WSBRO_APP_SECRET", "app-secret"),
			},
		}
	}

	return cfg
}

// Default returns a Config with sensible defaults, useful for testing.
func Default() *Config {
	return &Config{
		Port: "6001",
		Apps: []App{
			{
				ID:     "app-id",
				Key:    "app-key",
				Secret: "app-secret",
			},
		},
		MaxMessagesPerSecond: 100,
	}
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

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		n, err := strconv.Atoi(val)
		if err == nil {
			return n
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
