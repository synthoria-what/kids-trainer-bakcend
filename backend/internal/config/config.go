package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AdminLogin               string
	AdminPassword            string
	DevMode                  bool
	HTTPAddr                 string
	DatabasePath             string
	JWTSecret                string
	JWTTL                    time.Duration
	CORSOrigins              []string
	AIURL                    string
	AIToken                  string
	AIModel                  string
	AIFallbackURL            string
	AIFallbackToken          string
	AIFallbackModel          string
	AITimeout                time.Duration
	AIAnalyzeTimeout         time.Duration
	AIFallbackAnalyzeTimeout time.Duration
}

func Load() (*Config, error) {
	_ = godotenv.Load()
	cfg := &Config{
		AdminLogin:      getEnv("DEV_ADMIN_LOGIN", ""),
		AdminPassword:   getEnv("DEV_ADMIN_PASSWORD", ""),
		DevMode:         getEnv("APP_ENV", "production") == "development",
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DatabasePath:    getEnv("DATABASE_PATH", "./data/safe_route.db"),
		JWTSecret:       getEnv("JWT_SECRET", "dev-secret-change-me"),
		CORSOrigins:     splitEnv(getEnv("CORS_ORIGINS", "http://localhost:3000")),
		AIURL:           getEnv("AI_API_URL", "https://lenec.tech/v1/chat/completions"),
		AIToken:         getEnv("AI_API_TOKEN", ""),
		AIModel:         getEnv("AI_MODEL", ""),
		AIFallbackURL:   getEnv("AI_FALLBACK_API_URL", ""),
		AIFallbackToken: getEnv("AI_FALLBACK_API_TOKEN", ""),
		AIFallbackModel: getEnv("AI_FALLBACK_MODEL", ""),
	}
	if (cfg.AIFallbackURL != "" || cfg.AIFallbackToken != "" || cfg.AIFallbackModel != "") &&
		(cfg.AIFallbackURL == "" || cfg.AIFallbackToken == "" || cfg.AIFallbackModel == "") {
		return nil, fmt.Errorf("AI fallback requires AI_FALLBACK_API_URL, AI_FALLBACK_API_TOKEN and AI_FALLBACK_MODEL")
	}
	if ttl, err := strconv.Atoi(getEnv("JWT_TTL_HOURS", "168")); err == nil {
		cfg.JWTTL = time.Duration(ttl) * time.Hour
	} else {
		cfg.JWTTL = 168 * time.Hour
	}
	if timeout, err := strconv.Atoi(getEnv("AI_TIMEOUT_SECONDS", "60")); err == nil {
		cfg.AITimeout = time.Duration(timeout) * time.Second
	} else {
		cfg.AITimeout = 60 * time.Second
	}
	if analyzeTimeout, err := strconv.Atoi(getEnv("AI_ANALYZE_TIMEOUT_SECONDS", "20")); err == nil {
		cfg.AIAnalyzeTimeout = time.Duration(analyzeTimeout) * time.Second
	} else {
		cfg.AIAnalyzeTimeout = 20 * time.Second
	}
	fallbackTimeout, err := strconv.Atoi(getEnv("AI_FALLBACK_ANALYZE_TIMEOUT_SECONDS", "45"))
	if err != nil || fallbackTimeout <= 0 {
		return nil, fmt.Errorf("AI_FALLBACK_ANALYZE_TIMEOUT_SECONDS must be a positive integer")
	}
	cfg.AIFallbackAnalyzeTimeout = time.Duration(fallbackTimeout) * time.Second
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitEnv(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	cur := ""
	for _, c := range s {
		if c == ',' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
