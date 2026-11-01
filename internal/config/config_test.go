package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "6001" || cfg.AdapterDriver != "memory" || len(cfg.Apps) != 1 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if !cfg.UsesDefaultSecret() {
		t.Error("default secret should be detected")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("WSBRO_PORT", "7001")
	t.Setenv("WSBRO_APP_ID", "id")
	t.Setenv("WSBRO_APP_KEY", "key")
	t.Setenv("WSBRO_APP_SECRET", "secret")
	t.Setenv("WSBRO_MAX_CONNECTIONS", "500")
	t.Setenv("WSBRO_REDIS_DB", "3")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := App{ID: "id", Key: "key", Secret: "secret"}
	if cfg.Port != "7001" || cfg.MaxConnections != 500 || cfg.RedisDB != 3 || cfg.Apps[0] != want {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.UsesDefaultSecret() {
		t.Error("custom secret reported as default")
	}
}

func TestLoadMultipleApps(t *testing.T) {
	t.Setenv("WSBRO_APPS", `[{"id":"a","key":"ka","secret":"sa","max_connections":10},{"id":"b","key":"kb","secret":"sb"}]`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Apps) != 2 || cfg.Apps[0].MaxConnections != 10 || cfg.FindAppByKey("kb").ID != "b" || cfg.FindAppByID("a").Key != "ka" {
		t.Errorf("unexpected apps: %+v", cfg.Apps)
	}
	if cfg.FindAppByKey("nope") != nil || cfg.FindAppByID("nope") != nil {
		t.Error("unknown app found")
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"invalid number":    {"WSBRO_MAX_CONNECTIONS": "lots"},
		"invalid apps json": {"WSBRO_APPS": "{"},
		"empty apps":        {"WSBRO_APPS": "[]"},
		"missing secret":    {"WSBRO_APPS": `[{"id":"a","key":"k"}]`},
		"duplicate id":      {"WSBRO_APPS": `[{"id":"a","key":"k1","secret":"s"},{"id":"a","key":"k2","secret":"s"}]`},
		"duplicate key":     {"WSBRO_APPS": `[{"id":"a","key":"k","secret":"s"},{"id":"b","key":"k","secret":"s"}]`},
		"unknown adapter":   {"WSBRO_ADAPTER": "kafka"},
		"zero message size": {"WSBRO_MAX_MESSAGE_SIZE": "0"},
		"tls without key":   {"WSBRO_TLS_CERT": "/cert.pem"},
		"tls without cert":  {"WSBRO_TLS_KEY": "/key.pem"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestOriginPatterns(t *testing.T) {
	cfg := Default()
	app := &App{}

	if got := cfg.OriginPatterns(app); got != nil {
		t.Errorf("wildcard should allow any origin, got %v", got)
	}

	cfg.AllowedOrigins = "https://example.com, http://admin.example.com/ ,*.example.org"
	want := []string{"example.com", "admin.example.com", "*.example.org"}
	if got := cfg.OriginPatterns(app); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	app.AllowedOrigins = "app.test"
	if got := cfg.OriginPatterns(app); !reflect.DeepEqual(got, []string{"app.test"}) {
		t.Errorf("app setting should win, got %v", got)
	}
}

func TestValidateMessage(t *testing.T) {
	cfg := Default()
	cfg.Apps = append(cfg.Apps, cfg.Apps[0])
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("unexpected error: %v", err)
	}
}
