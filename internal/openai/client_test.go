package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nlink-jp/ask-llm-mcp/internal/toolerr"
)

// canned builds a minimal chat-completions JSON response carrying the
// given assistant content.
func canned(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": content}},
		},
	})
	return string(b)
}

// newTestClient builds a Client pointed at baseURL with a near-zero
// backoff so retry tests stay fast.
func newTestClient(t *testing.T, baseURL string, opt Options) *Client {
	t.Helper()
	opt.BaseURL = baseURL
	if opt.Model == "" {
		opt.Model = "test-model"
	}
	c, err := New(opt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.backoffBase = time.Millisecond
	c.backoffMax = time.Millisecond
	return c
}

func TestNew_Validation(t *testing.T) {
	if _, err := New(Options{BaseURL: "http://x/v1"}); err == nil {
		t.Error("expected model-required error")
	}
	if _, err := New(Options{Model: "m"}); err == nil {
		t.Error("expected base_url-required error")
	}
	c, err := New(Options{Model: "m", BaseURL: "http://x/v1/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.endpoint != "http://x/v1/chat/completions" {
		t.Errorf("endpoint = %q, want trailing slash trimmed", c.endpoint)
	}
}

func TestAsk_SuccessForwardsPromptAndPath(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, canned("hello back"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/v1", Options{})
	out, err := c.Ask(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if out != "hello back" {
		t.Errorf("out = %q, want hello back", out)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("auth header should be absent without api_key, got %q", gotAuth)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1 (no system prompt)", len(msgs))
	}
	user := msgs[0].(map[string]any)
	if user["role"] != "user" || user["content"] != "hi there" {
		t.Errorf("user message = %v", user)
	}
	if gotBody["stream"] != false {
		t.Errorf("stream = %v, want false", gotBody["stream"])
	}
	if _, ok := gotBody["temperature"]; ok {
		t.Error("temperature should be omitted when unset")
	}
	if _, ok := gotBody["max_tokens"]; ok {
		t.Error("max_tokens should be omitted when unset")
	}
}

func TestAsk_IncludesOptionalFields(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, canned("ok"))
	}))
	defer srv.Close()

	temp := 0.3
	maxTok := 256
	c := newTestClient(t, srv.URL+"/v1", Options{
		APIKey:       "sk-test",
		SystemPrompt: "be brief",
		Temperature:  &temp,
		MaxTokens:    &maxTok,
	})
	if _, err := c.Ask(context.Background(), "q"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("auth = %q, want Bearer sk-test", gotAuth)
	}
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2 (system + user)", len(msgs))
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "be brief" {
		t.Errorf("system message = %v", sys)
	}
	if v, _ := gotBody["temperature"].(float64); v != 0.3 {
		t.Errorf("temperature = %v, want 0.3", gotBody["temperature"])
	}
	if v, _ := gotBody["max_tokens"].(float64); v != 256 {
		t.Errorf("max_tokens = %v, want 256", gotBody["max_tokens"])
	}
}

func TestAsk_StripsReasoningBlocks(t *testing.T) {
	cases := map[string]string{
		"think tag":    "<think>chain\nof thought</think>final answer",
		"thinking tag": "<thinking>hmm</thinking>the answer",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, canned(content))
			}))
			defer srv.Close()
			c := newTestClient(t, srv.URL+"/v1", Options{})
			out, err := c.Ask(context.Background(), "q")
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if strings.Contains(out, "think") || strings.Contains(out, "chain") || strings.Contains(out, "hmm") {
				t.Errorf("reasoning not stripped: %q", out)
			}
		})
	}
}

func TestAsk_EmptyContentIsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, canned("   "))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL+"/v1", Options{})
	if _, err := c.Ask(context.Background(), "q"); !errors.Is(err, toolerr.New(toolerr.CodeUpstreamError, "")) {
		t.Errorf("want upstream_error, got %v", err)
	}
}

func TestAsk_NoChoicesIsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL+"/v1", Options{})
	if _, err := c.Ask(context.Background(), "q"); !errors.Is(err, toolerr.New(toolerr.CodeUpstreamError, "")) {
		t.Errorf("want upstream_error, got %v", err)
	}
}

func TestAsk_HTTP401NotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL+"/v1", Options{APIKey: "bad"})
	_, err := c.Ask(context.Background(), "q")
	if !errors.Is(err, toolerr.New(toolerr.CodeUpstreamError, "")) {
		t.Errorf("want upstream_error, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (401 is terminal, not retried)", got)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should surface upstream message: %v", err)
	}
}

func TestAsk_HTTP500ThenSuccessRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
			return
		}
		_, _ = io.WriteString(w, canned("recovered"))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL+"/v1", Options{})
	out, err := c.Ask(context.Background(), "q")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if out != "recovered" {
		t.Errorf("out = %q, want recovered", out)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (one retry after 500)", got)
	}
}

func TestAsk_ContextTimeoutIsUpstreamTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, canned("late"))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL+"/v1", Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Ask(ctx, "q"); !errors.Is(err, toolerr.New(toolerr.CodeUpstreamTimeout, "")) {
		t.Errorf("want upstream_timeout, got %v", err)
	}
}

func TestAsk_ConnectionRefusedHintsAtServer(t *testing.T) {
	// Spin a server, capture its URL, then close it so nothing listens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := newTestClient(t, url+"/v1", Options{})
	_, err := c.Ask(context.Background(), "q")
	if !errors.Is(err, toolerr.New(toolerr.CodeUpstreamError, "")) {
		t.Errorf("want upstream_error, got %v", err)
	}
	if !strings.Contains(err.Error(), "is the server running") {
		t.Errorf("error should hint the server is unreachable: %v", err)
	}
}
