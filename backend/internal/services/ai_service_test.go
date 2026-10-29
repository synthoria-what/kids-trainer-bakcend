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

func TestAnalyzeProviderFailover(t *testing.T) {
	const valid = `{"is_safe":true,"score":90,"summary":"Резервный ответ","tip":"Совет"}`
	for _, mode := range []string{"success", "http", "empty", "invalid", "partial", "timeout", "both fail", "canceled", "emit error", "reset error", "fallback only"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{AIURL: "https://primary.example", AIToken: "primary-token", AIModel: "primary-model",
				AIFallbackURL: "https://backup.example", AIFallbackToken: "backup-token", AIFallbackModel: "backup-model", AIAnalyzeTimeout: 10 * time.Millisecond}
			if mode == "fallback only" {
				cfg.AIToken = ""
			}
			svc := NewAIService(cfg)
			var calls []string
			svc.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Host)
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				token, model := "primary-token", "primary-model"
				if r.URL.Host == "backup.example" {
					token, model = "backup-token", "backup-model"
				}
				if r.Header.Get("Authorization") != "Bearer "+token || payload["model"] != model {
					t.Fatal("wrong provider credentials or model")
				}
				status, body := 200, ""
				if r.URL.Host == "primary.example" {
					switch mode {
					case "http", "both fail":
						status = 503
					case "invalid":
						body = "data: {broken}\n\n"
					case "partial", "emit error", "reset error":
						body = "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"choices\":[{\"finish_reason\":\"length\"}]}\n\n"
					case "timeout":
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
				}
				if mode == "success" || r.URL.Host == "backup.example" {
					data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": valid}}}})
					body = string(data)
				}
				if mode == "both fail" {
					status = 503
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			preview, resets := "", 0
			result, err := svc.AnalyzeStream(ctx, models.SafetyTask{}, "Ответ", func(piece string) error {
				if mode == "emit error" {
					return errors.New("disconnected")
				}
				preview += piece
				return nil
			}, func() error {
				resets++
				preview = ""
				if mode == "reset error" {
					return errors.New("disconnected")
				}
				return nil
			})
			wantCalls := 2
			if mode == "canceled" {
				wantCalls = 0
			} else if mode == "success" || mode == "emit error" || mode == "reset error" || mode == "fallback only" {
				wantCalls = 1
			}
			if len(calls) != wantCalls {
				t.Fatalf("calls=%v want %d", calls, wantCalls)
			}
			if mode == "both fail" || mode == "canceled" || mode == "emit error" || mode == "reset error" {
				if err == nil || result.Evaluated {
					t.Fatalf("expected failure, got %+v %v", result, err)
				}
				return
			}
			if err != nil || !result.Evaluated || preview != valid {
				t.Fatalf("result=%+v error=%v preview=%q", result, err, preview)
			}
			if (mode == "partial") != (resets == 1) {
				t.Fatalf("unexpected resets: %d", resets)
			}
		})
	}
}

func TestGenerateTasksFailover(t *testing.T) {
	svc := NewAIService(&config.Config{AIURL: "https://primary.example", AIToken: "a", AIModel: "a",
		AIFallbackURL: "https://backup.example", AIFallbackToken: "b", AIFallbackModel: "b"})
	calls := 0
	svc.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		content := `{"tasks":[]}` // A structurally valid but unusable primary answer.
		if r.URL.Host == "backup.example" {
			content = `{"tasks":[{"title":"Новое задание"}]}`
		}
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	tasks, err := svc.GenerateTasks(context.Background(), nil, models.GenerateTasksRequest{Count: 1})
	if err != nil || len(tasks) != 1 || calls != 2 {
		t.Fatalf("tasks=%v calls=%d err=%v", tasks, calls, err)
	}
}

func TestIndependentProviderAnalysisDeadlines(t *testing.T) {
	for _, shortParent := range []bool{false, true} {
		cfg := &config.Config{AIURL: "https://primary.example", AIToken: "a", AIModel: "a",
			AIFallbackURL: "https://backup.example", AIFallbackToken: "b", AIFallbackModel: "b",
			AIAnalyzeTimeout: 10 * time.Second, AIFallbackAnalyzeTimeout: 45 * time.Second}
		svc := NewAIService(cfg)
		ctx := context.Background()
		parentDeadline := time.Now().Add(time.Second)
		if shortParent {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, parentDeadline)
			defer cancel()
		}
		calls := 0
		svc.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			deadline, ok := r.Context().Deadline()
			if !ok {
				t.Fatal("missing analysis deadline")
			}
			if shortParent {
				if !deadline.Equal(parentDeadline) {
					t.Fatal("provider timeout extended parent deadline")
				}
			} else if r.URL.Host == "backup.example" {
				if remaining := time.Until(deadline); remaining <= 10*time.Second || remaining > 45*time.Second {
					t.Fatalf("fallback did not get its own timeout: %s", remaining)
				}
			} else if time.Until(deadline) > 10*time.Second {
				t.Fatal("primary deadline changed")
			}
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: http.NoBody}, nil
		})
		_, err := svc.Analyze(ctx, models.SafetyTask{}, "Ответ")
		if err == nil || calls != 2 || cfg.AIAnalyzeTimeout != 10*time.Second {
			t.Fatalf("err=%v calls=%d primary timeout=%s", err, calls, cfg.AIAnalyzeTimeout)
		}
	}
}

func TestIndependentProviderTokenBudgets(t *testing.T) {
	cfg := &config.Config{AIURL: "https://primary.example", AIToken: "a", AIModel: "a",
		AIFallbackURL: "https://backup.example", AIFallbackToken: "b", AIFallbackModel: "b",
		AIAnalyzeMaxTokens: 1024, AIFallbackAnalyzeMaxTokens: 4096}
	svc := NewAIService(cfg)
	calls := 0
	svc.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		want, status, body := float64(1024), 500, ""
		if r.URL.Host == "backup.example" {
			want, status = 4096, 200
			data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"is_safe":true,"score":90,"summary":"Ответ","tip":"Совет"}`}}}})
			body = string(data)
		}
		if payload["max_tokens"] != want {
			t.Fatalf("%s: max_tokens=%v, want %v", r.URL.Host, payload["max_tokens"], want)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result, err := svc.Analyze(context.Background(), models.SafetyTask{}, "Ответ")
	if err != nil || !result.Evaluated || calls != 2 || cfg.AIAnalyzeMaxTokens != 1024 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}
