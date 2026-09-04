package mcp

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
)

// Serve runs the codesearch MCP server on stdio until EOF.
//
// Each tool call resolves its target project from the tool's optional
// project_path argument, falling back to defaultCfg when set. Per-project
// index handles are held in an index.Pool: each is opened lazily on the
// first tool call for that project and released after an idle window
// (see index.LazyIndex). This lets a concurrent `codesearch sync` (e.g.
// from a git hook) acquire the writer lock between bursts of MCP traffic.
//
// defaultCfg may be nil. When it is, tools require project_path or
// return a clear error. When set (from `codesearch serve --root <path>`)
// its .codesearch/ must already exist.
func Serve(defaultCfg *config.IndexConfig) error {
	pool := index.NewPool(index.DefaultIdleTimeout, index.DefaultRetryBudget)
	defer func() { _ = pool.Close() }()

	srv := server.NewMCPServer(
		"codesearch",
		"0.1.0",
		server.WithToolCapabilities(false),
	)

	registerTools(srv, defaultCfg, pool)

	return server.ServeStdio(srv)
}
