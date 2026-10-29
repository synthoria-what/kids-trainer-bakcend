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
