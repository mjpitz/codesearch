package cmd

import (
	"context"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/mcp"
	"github.com/urfave/cli/v3"
)

// Serve runs the codesearch MCP server on stdio (for AI assistants).
var Serve = &cli.Command{
	Name:  "serve",
	Usage: "Run the codesearch MCP server on stdio (for AI assistants).",
	Action: func(ctx context.Context, c *cli.Command) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}

		return mcp.Serve(cfg)
	},
}
