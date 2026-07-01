package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/nlink-jp/ask-llm-mcp/internal/toolerr"
	"github.com/nlink-jp/ask-llm-mcp/internal/transport"
)

// pipeServer wires a Server up to an in-memory reader/writer pair and
// returns the server, the buffer that captured stdout, and a registered
// tool used in the routing tests.
func pipeServer(t *testing.T, in string, h ToolHandler) (*Server, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	tr := transport.NewStdioTransport(strings.NewReader(in), out)
	srv := New("test", "v0", tr, nil)
	if h != nil {
		srv.RegisterTool(Tool{
			Name:        "ask_llm",
			Description: "test",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, h)
	}
	return srv, out
}

// runOnce drives Serve until stdin EOF so we can inspect the buffer
// after a single request line.
func runOnce(t *testing.T, srv *Server) {
	t.Helper()
	if err := srv.Serve(context.Background()); err != nil && err != io.EOF {
		t.Fatalf("Serve: %v", err)
	}
}

// readResponses splits the captured stdout into one map per line.
func readResponses(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("response not JSON: %v (line=%q)", err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestHandleInitialize(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	srv, buf := pipeServer(t, req, nil)
	runOnce(t, srv)

	resps := readResponses(t, buf)
	if len(resps) != 1 {
		t.Fatalf("got %d responses, want 1", len(resps))
	}
	result, ok := resps[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %v", resps[0])
	}
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], ProtocolVersion)
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "test" || info["version"] != "v0" {
		t.Errorf("serverInfo = %v", info)
	}
}

func TestHandleToolsList_ReturnsRegisteredTool(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	srv, buf := pipeServer(t, req, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})
	runOnce(t, srv)

	result, _ := readResponses(t, buf)[0]["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	first, _ := tools[0].(map[string]any)
	if first["name"] != "ask_llm" {
		t.Errorf("tool name = %v", first["name"])
	}
}

func TestHandleToolsList_EmptyIsArrayNotNull(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":3,"method":"tools/list"}` + "\n"
	srv, buf := pipeServer(t, req, nil)
	runOnce(t, srv)

	// The wire form must contain "tools":[] not "tools":null so LLM
	// clients that iterate without nil checks do not crash.
	if !strings.Contains(buf.String(), `"tools":[]`) {
		t.Fatalf("expected empty array, got: %s", buf.String())
	}
}

func TestHandleToolsCall_SuccessReturnsTextContent(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":4,"method":"tools/call",` +
		`"params":{"name":"ask_llm","arguments":{"prompt":"hi"}}}` + "\n"
	srv, buf := pipeServer(t, req, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "hello back", nil
	})
	runOnce(t, srv)

	result, _ := readResponses(t, buf)[0]["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content len = %d", len(content))
	}
	first := content[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "hello back" {
		t.Errorf("content[0] = %v", first)
	}
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError = true on success path")
	}
}

func TestHandleToolsCall_StructuredErrorSerializesCodeJSON(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call",` +
		`"params":{"name":"ask_llm","arguments":{}}}` + "\n"
	srv, buf := pipeServer(t, req, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "", toolerr.New(toolerr.CodeInvalidArguments, "prompt must not be empty")
	})
	runOnce(t, srv)

	result, _ := readResponses(t, buf)[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("isError = false, want true")
	}
	content, _ := result["content"].([]any)
	first := content[0].(map[string]any)
	text, _ := first["text"].(string)

	var payload toolerr.Error
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("error text not JSON: %v (text=%q)", err, text)
	}
	if payload.Code != toolerr.CodeInvalidArguments {
		t.Errorf("code = %q, want %q", payload.Code, toolerr.CodeInvalidArguments)
	}
}

func TestHandleToolsCall_UnknownToolReturnsMethodNotFound(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":6,"method":"tools/call",` +
		`"params":{"name":"no_such_tool","arguments":{}}}` + "\n"
	srv, buf := pipeServer(t, req, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "unreachable", nil
	})
	runOnce(t, srv)

	resps := readResponses(t, buf)
	errObj, ok := resps[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got: %v", resps[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != -32601 {
		t.Errorf("code = %v, want -32601 (method not found)", errObj["code"])
	}
}

func TestHandleNotification_NoResponseEmitted(t *testing.T) {
	// notifications/initialized is sent after the initialize handshake
	// per MCP spec; it has no id, so the server must reply with
	// nothing at all (not an error, not a result).
	req := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	srv, buf := pipeServer(t, req, nil)
	runOnce(t, srv)

	if buf.Len() != 0 {
		t.Errorf("notification produced output: %q", buf.String())
	}
}

func TestHandleParseError_NoIDStillEmitsError(t *testing.T) {
	req := "not-json\n"
	srv, buf := pipeServer(t, req, nil)
	runOnce(t, srv)

	resps := readResponses(t, buf)
	errObj, ok := resps[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got: %v", resps[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != -32700 {
		t.Errorf("code = %v, want -32700 (parse error)", errObj["code"])
	}
}
