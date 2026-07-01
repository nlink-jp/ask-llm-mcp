//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestErrors_EmptyPromptReturnsStructuredInvalidArguments confirms the
// strict argument validation in tools/ask_llm.go reaches the wire. The
// handler must reject before the request ever touches the upstream.
func TestErrors_EmptyPromptReturnsStructuredInvalidArguments(t *testing.T) {
	h := Start(t)
	mustInit(t, h)

	text, isErr, err := h.CallTool("ask_llm", map[string]any{
		"prompt": "   ",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !isErr {
		t.Fatalf("isError = false; payload: %s", text)
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("error payload not JSON: %v (text=%q)", err, text)
	}
	if payload.Code != "invalid_arguments" {
		t.Errorf("code = %q, want invalid_arguments", payload.Code)
	}
	if !strings.Contains(payload.Message, "prompt") {
		t.Errorf("message did not mention prompt: %q", payload.Message)
	}
}

// TestErrors_UnknownToolReturnsRPCError exercises tools/call with an
// unregistered name to confirm the server's method-not-found path
// reaches the wire correctly.
func TestErrors_UnknownToolReturnsRPCError(t *testing.T) {
	h := Start(t)
	mustInit(t, h)

	_, _, err := h.CallTool("no_such_tool", map[string]any{"x": 1}, 5*time.Second)
	if err == nil {
		t.Fatal("expected RPC error for unknown tool")
	}
	if !strings.Contains(err.Error(), "method not found") &&
		!strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("unexpected error wording: %v", err)
	}
}

// TestErrors_TimeoutForcesUpstreamTimeout points the binary at a dummy
// server that stalls longer than the per-request timeout. With
// ASK_LLM_REQUEST_TIMEOUT=1 the call must surface as upstream_timeout.
func TestErrors_TimeoutForcesUpstreamTimeout(t *testing.T) {
	url := dummyServer(t, slowReply(5*time.Second, "too late"))
	h := StartWithServer(t, url, map[string]string{
		"ASK_LLM_REQUEST_TIMEOUT": "1",
	})
	mustInit(t, h)

	text, isErr, err := h.CallTool("ask_llm", map[string]any{
		"prompt": "this will time out",
	}, 20*time.Second)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !isErr {
		t.Fatalf("isError = false; payload: %s", text)
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("error payload not JSON: %v (text=%q)", err, text)
	}
	if payload.Code != "upstream_timeout" {
		t.Errorf("code = %q, want upstream_timeout", payload.Code)
	}
}

// mustInit drives the MCP initialize handshake before exercising any
// tool call.
func mustInit(t *testing.T, h *Harness) {
	t.Helper()
	if _, err := h.Call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
	}, 10*time.Second); err != nil {
		t.Fatalf("initialize: %v", err)
	}
}
