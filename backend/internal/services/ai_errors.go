package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func aiHTTPError(res *http.Response) error {
	var body map[string]any
	if json.NewDecoder(io.LimitReader(res.Body, 16*1024)).Decode(&body) == nil {
		if value := body["error"]; value != nil {
			return fmt.Errorf("AI returned status %d: %w", res.StatusCode, aiProviderError(value))
		}
	}
	return fmt.Errorf("AI returned status %d", res.StatusCode)
}

// Provider messages may echo credentials or a child's answer. Log only known
// error identifiers and a coarse category inferred from the message, never the
// original message or unknown identifiers.
func aiProviderError(value any) error {
	code, kind, param, message := "", "", "", ""
	switch detail := value.(type) {
	case map[string]any:
		code = aiErrorIdentifier(detail["code"])
		kind = aiErrorIdentifier(detail["type"])
		message, _ = detail["message"].(string)
		if field, ok := detail["param"].(string); ok {
			switch field {
			case "model", "stream", "response_format", "max_tokens", "max_completion_tokens", "temperature", "top_p", "messages":
				param = field
			default:
				param = "other"
			}
		}
	case string:
		message = detail
	}
	category := "unknown"
	text := strings.ToLower(code + " " + kind + " " + message)
	for _, rule := range []struct {
		category string
		patterns []string
	}{
		{"authentication", []string{"invalid_api_key", "invalid api key", "incorrect api key", "unauthorized", "authentication"}},
		{"quota", []string{"quota", "credit", "balance", "billing"}},
		{"rate_limit", []string{"rate_limit", "rate limit", "too many requests"}},
		{"model_unavailable", []string{"model_not_found", "model not found", "model_not_available", "model unavailable", "no available channel"}},
		{"unsupported_request", []string{"unsupported", "not supported", "invalid_parameter", "invalid parameter", "invalid_request", "invalid request"}},
		{"timeout", []string{"timeout", "timed out"}},
		{"upstream_unavailable", []string{"overload", "unavailable", "upstream_error", "server_error", "internal error", "internal_error"}},
	} {
		matched := false
		for _, pattern := range rule.patterns {
			if strings.Contains(text, pattern) {
				category, matched = rule.category, true
				break
			}
		}
		if matched {
			break
		}
	}
	return fmt.Errorf("AI provider returned an error response (code=%q type=%q param=%q category=%q)", code, kind, param, category)
}

func aiErrorIdentifier(value any) string {
	if value == nil {
		return ""
	}
	text := strings.ToLower(fmt.Sprint(value))
	if n, err := strconv.Atoi(text); err == nil && n >= 100 && n <= 599 {
		return strconv.Itoa(n)
	}
	switch text {
	case "invalid_api_key", "invalid_key", "unauthorized", "authentication_error", "authentication_failed",
		"insufficient_quota", "quota_exceeded", "billing_error", "rate_limit_exceeded", "rate_limit_error",
		"model_not_found", "model_not_available", "model_not_supported", "invalid_request_error", "invalid_request",
		"invalid_argument", "invalid_parameter", "unsupported_parameter", "unsupported_value", "unsupported_response_format",
		"context_length_exceeded", "content_filter", "content_policy_violation", "server_error", "internal_error",
		"internal_server_error", "service_unavailable", "overloaded_error", "overloaded", "timeout", "request_timeout",
		"upstream_error", "provider_error", "api_error", "permission_denied", "forbidden", "not_found", "bad_request_error":
		return text
	}
	return "other"
}
