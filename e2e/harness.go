//go:build e2e

// Package e2e drives the ask-llm-mcp binary via JSON-RPC over stdio,
// simulating a real MCP client. These tests are excluded from
// `go test ./...`; run them with:
//
//	make test-e2e
//
// They are hermetic: instead of a real LLM, each test stands up an
// in-process httptest server that speaks the OpenAI /chat/completions
// API and points the spawned binary at it via ASK_LLM_BASE_URL. No
// network, credentials, or running LM Studio are required.
// ASK_LLM_TEST_BINARY must point at the built binary (defaults to
// ./dist/ask-llm-mcp, which `make test-e2e` builds for you).
package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Harness drives a spawned ask-llm-mcp process over stdio.
type Harness struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	nextID atomic.Int64
}

// dummyServer starts an httptest server with the given handler and
// registers cleanup. Returns its base URL (the binary appends
// /chat/completions).
func dummyServer(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// cannedReply returns a handler that answers /chat/completions with a
// fixed assistant message.
func cannedReply(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
			},
		})
	}
}

// slowReply answers after d, or bails out early if the client
// disconnects (so the per-request timeout path can be exercised without
// blocking server shutdown).
func slowReply(d time.Duration, content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
			},
		})
	}
}

// Start spawns the binary wired to a default dummy server that replies
// "pong". Use StartWithServer for custom upstream behaviour.
func Start(t *testing.T, args ...string) *Harness {
	url := dummyServer(t, cannedReply("pong"))
	return StartWithServer(t, url, nil, args...)
}

// StartWithServer spawns the binary configured to talk to serverURL.
// extraEnv layers additional env vars (e.g. ASK_LLM_REQUEST_TIMEOUT) on
// top of the hermetic base config.
func StartWithServer(t *testing.T, serverURL string, extraEnv map[string]string, args ...string) *Harness {
	t.Helper()

	binary := os.Getenv("ASK_LLM_TEST_BINARY")
	if binary == "" {
		t.Skip("ASK_LLM_TEST_BINARY not set; run via `make test-e2e`")
	}

	env := map[string]string{
		"ASK_LLM_BASE_URL": serverURL,
		"ASK_LLM_MODEL":    "dummy-model",
	}
	for k, v := range extraEnv {
		env[k] = v
	}

	cmd := exec.Command(binary, args...)
	cmd.Env = buildEnv(env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	// Server logs go to stderr; surface them with `go test -v`.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start binary: %v", err)
	}

	lines := make(chan []byte, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			b := make([]byte, len(scanner.Bytes()))
			copy(b, scanner.Bytes())
			lines <- b
		}
	}()

	h := &Harness{t: t, cmd: cmd, stdin: stdin, lines: lines}
	t.Cleanup(h.Close)
	return h
}

// buildEnv starts from the process environment, strips any pre-existing
// ASK_LLM_* / OPENAI_* keys so the developer's shell cannot leak into
// the hermetic run, then layers the given overrides.
func buildEnv(overrides map[string]string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ASK_LLM_") || strings.HasPrefix(kv, "OPENAI_") {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}

// Close gracefully shuts the server down by closing stdin and waiting.
func (h *Harness) Close() {
	_ = h.stdin.Close()
	_ = h.cmd.Wait()
}

// Call sends a JSON-RPC request and waits up to timeout for the matching
// response.
func (h *Harness) Call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	id := h.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	if err := json.NewEncoder(h.stdin).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-h.lines:
			if !ok {
				return nil, fmt.Errorf("server stdout closed before response (method=%s id=%d)", method, id)
			}
			var resp struct {
				ID     json.Number     `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(line, &resp); err != nil {
				return nil, fmt.Errorf("parse response: %w (line=%q)", err, string(line))
			}
			respID, err := resp.ID.Int64()
			if err != nil || respID != id {
				continue
			}
			if resp.Error != nil {
				return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		case <-deadline:
			return nil, fmt.Errorf("timeout after %v waiting for response (method=%s id=%d)", timeout, method, id)
		}
	}
}

// CallTool is a convenience helper for tools/call: it returns the inner
// text from the first content block and the isError flag.
func (h *Harness) CallTool(name string, arguments any, timeout time.Duration) (string, bool, error) {
	res, err := h.Call("tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	}, timeout)
	if err != nil {
		return "", false, err
	}
	var wrap struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &wrap); err != nil {
		return "", false, fmt.Errorf("parse tools/call result: %w", err)
	}
	if len(wrap.Content) == 0 {
		return "", wrap.IsError, fmt.Errorf("tools/call returned no content")
	}
	return wrap.Content[0].Text, wrap.IsError, nil
}
