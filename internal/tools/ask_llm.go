// Package tools registers the MCP tools exposed by ask-llm-mcp.
//
// The server exposes exactly one tool, ask_llm, that forwards a single
// prompt to the configured OpenAI-compatible endpoint and returns the
// response. Use-case-specific tools (review / discuss / factcheck …)
// were deliberately not added — keeping the tool surface minimal matches
// the project's stateless transparent-pipe model. Which model/endpoint
// answers is fixed by config, so switching backends means running a
// second server with a different --config file, not a second tool.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/nlink-jp/ask-llm-mcp/internal/mcpserver"
	"github.com/nlink-jp/ask-llm-mcp/internal/toolerr"
)

// Asker is the abstraction the handler depends on. Production wires it
// to *openai.Client; tests substitute a fake.
type Asker interface {
	Ask(ctx context.Context, prompt string) (string, error)
}

// askLLMInputSchema is the MCP inputSchema advertised in tools/list.
// Clients validate arguments against this before calling.
var askLLMInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": {
      "type": "string",
      "description": "The question or consultation to send to the LLM. Free-form text. Include relevant context, background, and your current thinking — the LLM does not see the surrounding conversation."
    }
  },
  "required": ["prompt"],
  "additionalProperties": false
}`)

const askLLMDescription = `Ask a local (or OpenAI-compatible) LLM for a second opinion. ` +
	`Forwards the prompt to the configured chat-completions endpoint ` +
	`(e.g. a local LM Studio server) and returns its response. ` +
	`Stateless: each call is independent. Include any context needed in the prompt itself.`

// AskLLMTool returns the MCP tool descriptor.
func AskLLMTool() mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "ask_llm",
		Description: askLLMDescription,
		InputSchema: askLLMInputSchema,
	}
}

// askLLMArgs is the JSON shape of the tool arguments. Decoded strictly
// (unknown fields rejected) so a misspelled key surfaces as an
// invalid_arguments error rather than being silently ignored
// (feedback_strict_json_decode.md).
type askLLMArgs struct {
	Prompt string `json:"prompt"`
}

// AskLLMHandler returns an MCP tool handler bound to asker. Wire the
// returned function into mcpserver via RegisterTool.
func AskLLMHandler(asker Asker) mcpserver.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args askLLMArgs
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&args); err != nil {
			return "", toolerr.New(toolerr.CodeInvalidArguments, "invalid arguments: "+err.Error())
		}
		if strings.TrimSpace(args.Prompt) == "" {
			return "", toolerr.New(toolerr.CodeInvalidArguments, "prompt must not be empty")
		}
		return asker.Ask(ctx, args.Prompt)
	}
}
