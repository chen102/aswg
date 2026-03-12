package server

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host                    string
	Port                    int
	AuthToken               string
	EnabledAdapters         []string
	DefaultAdapter          string
	Version                 string
	SeedFile                string
	FrontendDir             string
	SessionMetaMapFile      string
	RateLimitSessionsPerSec int
}

func LoadConfig() Config {
	port := 8080
	if raw := strings.TrimSpace(os.Getenv("SERVER_PORT")); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			port = p
		}
	}

	host := strings.TrimSpace(os.Getenv("SERVER_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}

	defaultAdapter := strings.TrimSpace(os.Getenv("DEFAULT_ADAPTER"))
	if defaultAdapter == "" {
		defaultAdapter = "codex"
	}

	seedFile := strings.TrimSpace(os.Getenv("CODEX_SEED_FILE"))
	if seedFile == "" {
		seedFile = "docs/resume-smoke.jsonl"
	}

	frontendDir := strings.TrimSpace(os.Getenv("FRONTEND_DIR"))
	if frontendDir == "" {
		frontendDir = "frontend/src"
	}

	sessionMetaMapFile := strings.TrimSpace(os.Getenv("SESSION_META_MAP_FILE"))
	if sessionMetaMapFile == "" {
		sessionMetaMapFile = ".run/session-meta-map.json"
	}

	enabledAdapters := splitCSV(strings.TrimSpace(os.Getenv("ENABLED_ADAPTERS")))
	if len(enabledAdapters) == 0 {
		enabledAdapters = []string{"codex"}
	}

	version := strings.TrimSpace(os.Getenv("APP_VERSION"))
	if version == "" {
		version = "0.1.0"
	}

	rateLimitSessionsPerSec := 30
	if raw := strings.TrimSpace(os.Getenv("RATE_LIMIT_SESSIONS_PER_SEC")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			rateLimitSessionsPerSec = n
		}
	}

	return Config{
		Host:                    host,
		Port:                    port,
		AuthToken:               strings.TrimSpace(os.Getenv("AUTH_TOKEN")),
		EnabledAdapters:         enabledAdapters,
		DefaultAdapter:          defaultAdapter,
		Version:                 version,
		SeedFile:                seedFile,
		FrontendDir:             frontendDir,
		SessionMetaMapFile:      sessionMetaMapFile,
		RateLimitSessionsPerSec: rateLimitSessionsPerSec,
	}
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	items := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		items = append(items, s)
	}
	return items
}
