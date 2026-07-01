// Package cmd defines the ask-llm-mcp CLI surface via cobra.
//
// ask-llm-mcp is a generic OpenAI-compatible LLM consultation MCP
// server. It speaks the MCP protocol over stdio and exposes a single
// tool, ask_llm, that MCP clients (Claude Code / Desktop etc.) can use
// to ask a local (or OpenAI-compatible) model for a second opinion. The
// primary target is a locally running LM Studio server.
package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nlink-jp/ask-llm-mcp/internal/config"
	"github.com/nlink-jp/ask-llm-mcp/internal/mcpserver"
	"github.com/nlink-jp/ask-llm-mcp/internal/openai"
	"github.com/nlink-jp/ask-llm-mcp/internal/tools"
	"github.com/nlink-jp/ask-llm-mcp/internal/transport"
)

// logLevel maps a level name to slog.Level; an unknown or empty value
// falls back to slog.LevelInfo so a misspelled setting does not silently
// lose all visibility.
func logLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

var (
	flagConfig    string
	serverVersion = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "ask-llm-mcp",
	Short: "MCP server that forwards prompts to an OpenAI-compatible LLM",
	Long: `ask-llm-mcp is an MCP stdio server that exposes a single tool,
ask_llm(prompt), which forwards the prompt to an OpenAI API-compatible
chat-completions endpoint and returns the response. The primary target
is a locally running LM Studio server, so you can get a second opinion
from a local model with no cloud billing and no data leaving the machine.

Which model/endpoint answers is fixed by config. To offer more than one
backend, run several servers with different --config files.

Configuration lives at ~/.config/ask-llm-mcp/config.toml; see
config.example.toml for the schema. Override the path with --config.`,
	RunE: run,
}

// Execute runs the root command. Called from main.go with the
// build-time version string injected via -ldflags.
func Execute(version string) {
	serverVersion = version
	rootCmd.Version = version
	rootCmd.Flags().StringVarP(&flagConfig, "config", "c", "",
		"Config file path (default: ~/.config/ask-llm-mcp/config.toml)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// run wires the MCP server: load config, create the OpenAI-compatible
// client, register the ask_llm tool, and serve over stdio until
// SIGINT / SIGTERM or stdin EOF.
//
// Logging goes to stderr only. stdout is owned by the MCP transport;
// anything else written there would break the JSON-RPC framing.
func run(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(cfg.Log.Level),
	}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	client, err := openai.New(openai.Options{
		BaseURL:      cfg.LLM.BaseURL,
		APIKey:       cfg.LLM.APIKey,
		Model:        cfg.LLM.Model,
		SystemPrompt: cfg.LLM.SystemPrompt,
		Temperature:  cfg.LLM.Temperature,
		MaxTokens:    cfg.LLM.MaxTokens,
		TimeoutSec:   cfg.LLM.RequestTimeout,
	})
	if err != nil {
		return err
	}
	client.SetLogger(logger)

	logger.Info("ask-llm-mcp ready",
		"base_url", cfg.LLM.BaseURL,
		"model", cfg.LLM.Model,
		"auth", cfg.LLM.APIKey != "",
		"request_timeout_s", cfg.LLM.RequestTimeout)

	tr := transport.NewStdioTransport(os.Stdin, os.Stdout)
	srv := mcpserver.New("ask-llm-mcp", serverVersion, tr, logger)
	srv.RegisterTool(tools.AskLLMTool(), tools.AskLLMHandler(client))

	if err := srv.Serve(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}
