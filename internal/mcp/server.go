package mcp

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
)

// Serve opens the index in read-only mode and serves the codesearch tool
// suite over stdio until EOF. cfg.Root and cfg.IndexPath() must already
// exist (run `codesearch init && codesearch sync` first). Read-only mode
// lets a concurrent `codesearch sync` (e.g. from a git hook) hold the
// writer lock without blocking the server.
func Serve(cfg *config.IndexConfig) error {
	idx, err := index.OpenReadOnly(cfg.IndexPath())
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	srv := server.NewMCPServer(
		"codesearch",
		"0.1.0",
		server.WithToolCapabilities(false),
	)

	registerTools(srv, cfg, idx)

	return server.ServeStdio(srv)
}
