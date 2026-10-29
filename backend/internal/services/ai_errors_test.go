package services

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAIProviderErrorDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"authentication", `{"code":"invalid_api_key","type":"authentication_error","message":"invalid api key: private-secret"}`, `code="invalid_api_key" type="authentication_error"`},
		{"quota", `{"code":"insufficient_quota","message":"private account has no credits"}`, `category="quota"`},
		{"rate limit", `{"code":429,"message":"Too many requests from private account"}`, `code="429"`},
		{"unsupported request", `{"type":"invalid_request_error","param":"response_format","message":"response_format is not supported: private prompt"}`, `param="response_format" category="unsupported_request"`},
		{"timeout string", `"upstream timed out: private child answer"`, `category="timeout"`},
		{"model", `{"code":"model_not_found","message":"private model not found"}`, `category="model_unavailable"`},
		{"unknown", `{"code":"private-secret","type":"private-child-answer","param":"private-input","message":"private details"}`, `code="other" type="other" param="other" category="unknown"`},
		{"numeric unknown", `{"code":123456789,"message":"private"}`, `code="other"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal([]byte(tc.body), &value); err != nil {
				t.Fatal(err)
			}
			err := aiResponseError(map[string]any{"error": value})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatalf("error exposes private upstream data: %v", err)
			}
		})
	}
}

func TestAIHTTPErrorDiagnostics(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"server_error","message":"private upstream details"}}`,
		`<html>private gateway details</html>`,
		strings.Repeat("private", 4096),
	} {
		res := &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(body))}
		err := aiHTTPError(res)
		if !strings.Contains(err.Error(), "AI returned status 500") || strings.Contains(err.Error(), "private") {
			t.Fatalf("unexpected HTTP diagnostic: %v", err)
		}
		if strings.Contains(body, "server_error") && !strings.Contains(err.Error(), `code="server_error"`) {
			t.Fatalf("lost upstream error code: %v", err)
		}
	}
}
