package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/mcp"
	"github.com/urfave/cli/v3"
)

// Serve runs the codesearch MCP server on stdio (for AI assistants).
var Serve = &cli.Command{
	Name:  "serve",
	Usage: "Run the codesearch MCP server on stdio (for AI assistants).",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "root",
			Usage: "optional default project root. When set, tool calls that omit " +
				"project_path use this repo. When unset, every tool call must supply " +
				"project_path.",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		root := c.String("root")
		if strings.Contains(root, "${") {
			return fmt.Errorf("--root contains an unexpanded variable (%q); "+
				"some MCP hosts (e.g. Cline) don't substitute variables like "+
				"${workspaceFolder} in their MCP config — pass a hardcoded "+
				"absolute path instead, or omit --root and rely on per-tool "+
				"project_path", root)
		}

		var defaultCfg *config.IndexConfig
		if root != "" {
			var err error
			defaultCfg, err = config.Load(root)
			if err != nil {
				return err
			}
		}

		return mcp.Serve(defaultCfg)
	},
}
