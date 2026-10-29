package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kids-trainer/backend/internal/config"
	"github.com/kids-trainer/backend/internal/models"
)

type aiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f aiRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAnalyzeAIResponses(t *testing.T) {
	const answer = `{"is_safe":true,"score":90,"summary":"Хороший ответ","tip":"Позови взрослого"}`
	completion, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": answer}, "finish_reason": "stop",
	}}})
	delta := func(piece string) string {
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": piece}}}})
		return "data: " + string(data) + "\n\n"
	}
	finish := func(reason string) string {
		return fmt.Sprintf("data: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\n", reason)
	}
	for _, tc := range []struct {
		name, contentType, body, wantError string
		stream                             bool
	}{
		{name: "stream chunks", stream: true, contentType: "text/event-stream", body: ": keepalive\n\ndata: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" + delta(answer[:30]) + delta(answer[30:]) + finish("stop") + "data: [DONE]\n\n"},
		{name: "JSON despite stream request", stream: true, contentType: "application/json", body: string(completion)},
		{name: "JSON without content type", stream: true, body: " \n" + string(completion)},
		{name: "non-stream JSON", contentType: "application/json", body: string(completion)},
		{name: "reasoning exhausts budget", stream: true, body: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private reasoning\"}}]}\n\n" + finish("length"), wantError: `finish_reason="length"`},
		{name: "truncated answer", stream: true, body: delta(answer[:20]) + finish("length"), wantError: `finish_reason="length"`},
		{name: "filtered", stream: true, body: finish("content_filter"), wantError: `finish_reason="content_filter"`},
		{name: "provider SSE error", stream: true, body: "data: {\"error\":{\"message\":\"private upstream detail\"}}\n\n", wantError: "AI provider returned an error response"},
		{name: "provider JSON error", stream: true, contentType: "application/json", body: `{"error":{"message":"private upstream detail"}}`, wantError: "AI provider returned an error response"},
		{name: "malformed event", stream: true, body: "data: {broken}\n\n", wantError: "invalid AI stream event"},
		{name: "no content", stream: true, body: finish("stop"), wantError: `no message content (finish_reason="stop")`},
		{name: "empty body", stream: true, wantError: "empty body"},
		{name: "event at EOF", stream: true, body: strings.TrimSuffix(delta(answer), "\n\n")},
		{name: "multiline event", stream: true, body: "data: {\"choices\":\n" + "data: [{\"delta\":{\"content\":" + fmt.Sprintf("%q", answer) + "}}]}\n\n"},
		{name: "incomplete analysis", stream: true, body: delta(`{"is_safe":true,"score":90}`), wantError: "incomplete AI analysis"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewAIService(&config.Config{AIURL: "https://ai.example/chat/completions", AIToken: "test", AIModel: "test", AITimeout: time.Second})
			svc.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["stream"] != tc.stream || payload["max_tokens"].(float64) < 1024 {
					t.Errorf("unexpected request stream or token budget")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			var emitted strings.Builder
			var emit func(string) error
			if tc.stream {
				emit = func(piece string) error { emitted.WriteString(piece); return nil }
			}
			result, err := svc.analyze(context.Background(), models.SafetyTask{}, "Позову взрослого", emit)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("got %v, want %q", err, tc.wantError)
				}
				if strings.Contains(err.Error(), "private") || strings.Contains(emitted.String(), "private") {
					t.Fatal("private upstream details exposed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !result.Evaluated || result.IsDemo || result.Score != 90 || !result.IsSafe {
				t.Fatalf("unexpected analysis: %+v", result)
			}
			if tc.stream && emitted.String() != answer {
				t.Fatalf("emitted %q, want %q", emitted.String(), answer)
			}
		})
	}
}

func TestReadAIResponseStopsOnEmitError(t *testing.T) {
	want := errors.New("client disconnected")
	res := &http.Response{Header: http.Header{}, Body: http.NoBody}
	for _, body := range []string{
		`{"choices":[{"message":{"content":"answer"}}]}`,
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n",
	} {
		res.Body = io.NopCloser(strings.NewReader(body))
		_, err := readAIResponse(res, func(string) error { return want })
		if !errors.Is(err, want) {
			t.Fatalf("got %v, want emit error", err)
		}
	}
}
