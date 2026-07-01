package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nlink-jp/ask-llm-mcp/internal/jsonrpc"
	"github.com/nlink-jp/ask-llm-mcp/internal/toolerr"
)

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

func (s *Server) handleToolsList(req jsonrpc.Request) error {
	// Always return a non-nil slice so the JSON has [] not null.
	tools := s.tools
	if tools == nil {
		tools = []Tool{}
	}
	return s.writeResult(req.ID, toolsListResult{Tools: tools})
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ContentBlock is one block in the tools/call result.content array.
// MCP spec 2024-11-05.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type toolsCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

func (s *Server) handleToolsCall(ctx context.Context, req jsonrpc.Request) error {
	var p toolsCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return s.writeError(req.ID, jsonrpc.CodeInvalidParams, "invalid params: "+err.Error())
	}
	h, ok := s.handlers[p.Name]
	if !ok {
		return s.writeError(req.ID, jsonrpc.CodeMethodNotFound, "unknown tool: "+p.Name)
	}

	start := time.Now()
	s.logger.Debug("tool call begin", "tool", p.Name)
	out, err := h(ctx, p.Arguments)
	latency := time.Since(start).Round(time.Millisecond)
	if err != nil {
		s.logger.Info("tool call failed", "tool", p.Name, "latency", latency, "err", err)
		return s.writeToolError(req, err)
	}
	s.logger.Info("tool call ok", "tool", p.Name, "latency", latency, "bytes", len(out))
	return s.writeResult(req.ID, toolsCallResult{
		Content: []ContentBlock{{Type: "text", Text: out}},
	})
}

// writeToolError emits a tool error per MCP convention: result with
// isError=true and a single text content block. If err is (or wraps) a
// *toolerr.Error, the content carries the structured {code, message,
// details} JSON so LLM clients can branch on the code. Otherwise the
// plain Error() string is used.
func (s *Server) writeToolError(req jsonrpc.Request, err error) error {
	var te *toolerr.Error
	if errors.As(err, &te) {
		body, marshalErr := json.Marshal(te)
		if marshalErr == nil {
			return s.writeResult(req.ID, toolsCallResult{
				IsError: true,
				Content: []ContentBlock{{Type: "text", Text: string(body)}},
			})
		}
		// Fall through to plain text on marshal failure.
	}
	return s.writeResult(req.ID, toolsCallResult{
		IsError: true,
		Content: []ContentBlock{{Type: "text", Text: err.Error()}},
	})
}
