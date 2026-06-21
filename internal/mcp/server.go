package mcp

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
)

// Serve runs the codesearch MCP server on stdio until EOF.
//
// The index is opened lazily on the first tool call and released after
// an idle window (see index.LazyIndex). This is what lets a concurrent
// `codesearch sync` (e.g. from a git hook) acquire the writer lock
// between bursts of MCP traffic — the server doesn't hoard the OS file
// lock when no tool calls are in flight.
//
// cfg.Root and cfg.IndexPath() must already exist (run `codesearch init
// && codesearch sync` first).
func Serve(cfg *config.IndexConfig) error {
	lazy := index.NewLazy(cfg.IndexPath(), index.DefaultIdleTimeout, index.DefaultRetryBudget)
	defer func() { _ = lazy.Close() }()

	srv := server.NewMCPServer(
		"codesearch",
		"0.1.0",
		server.WithToolCapabilities(false),
	)

	registerTools(srv, cfg, lazy)

	return server.ServeStdio(srv)
}
