package config

import (
	"testing"
	"time"
)

func TestFallbackAnalysisTimeout(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", 45 * time.Second, false},
		{"50", 50 * time.Second, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"invalid", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("AI_FALLBACK_API_URL", "")
			t.Setenv("AI_FALLBACK_API_TOKEN", "")
			t.Setenv("AI_FALLBACK_MODEL", "")
			t.Setenv("AI_ANALYZE_TIMEOUT_SECONDS", "20")
			t.Setenv("AI_ANALYZE_MAX_TOKENS", "")
			t.Setenv("AI_FALLBACK_ANALYZE_MAX_TOKENS", "")
			t.Setenv("AI_FALLBACK_ANALYZE_TIMEOUT_SECONDS", tc.value)
			cfg, err := Load()
			if tc.invalid {
				if err == nil {
					t.Fatal("expected invalid timeout error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AIAnalyzeTimeout != 20*time.Second || cfg.AIFallbackAnalyzeTimeout != tc.want {
				t.Fatal("provider timeouts are not independent")
			}
		})
	}
}

func TestAnalysisTokenBudgets(t *testing.T) {
	for _, key := range []string{"AI_ANALYZE_MAX_TOKENS", "AI_FALLBACK_ANALYZE_MAX_TOKENS"} {
		for _, value := range []string{"", "2048", "0", "-1", "bad"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				for _, env := range []string{"AI_FALLBACK_API_URL", "AI_FALLBACK_API_TOKEN", "AI_FALLBACK_MODEL", "AI_ANALYZE_MAX_TOKENS", "AI_FALLBACK_ANALYZE_MAX_TOKENS"} {
					t.Setenv(env, "")
				}
				t.Setenv("AI_FALLBACK_ANALYZE_TIMEOUT_SECONDS", "45")
				t.Setenv(key, value)
				cfg, err := Load()
				if value != "" && value != "2048" {
					if err == nil {
						t.Fatal("invalid token budget accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				primary, backup := 1024, 4096
				if value == "2048" {
					if key == "AI_ANALYZE_MAX_TOKENS" {
						primary = 2048
					} else {
						backup = 2048
					}
				}
				if cfg.AIAnalyzeMaxTokens != primary || cfg.AIFallbackAnalyzeMaxTokens != backup {
					t.Fatalf("unexpected budgets: %d, %d", cfg.AIAnalyzeMaxTokens, cfg.AIFallbackAnalyzeMaxTokens)
				}
			})
		}
	}
}
