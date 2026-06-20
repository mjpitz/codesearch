package cmd

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/mjpitz/codesearch/internal/query"
	"github.com/urfave/cli/v3"
)

// Query searches the index for the given terms.
var Query = &cli.Command{
	Name:  "query",
	Usage: "Search the index for the given terms.",
	Flags: []cli.Flag{
		&cli.StringMapFlag{
			Name:  "fields",
			Usage: "filter `key=value` (repeatable, AND)",
		},
		&cli.IntFlag{
			Name:  "limit",
			Usage: "max hits to return",
			Value: 10,
		},
		&cli.StringFlag{
			Name:  "format",
			Usage: "output format: text or json",
			Value: "text",
		},
		&cli.BoolFlag{
			Name:  "no-snippet",
			Usage: "skip snippet generation",
			Value: false,
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		fields := c.Value("fields").(map[string]string)
		limit := c.Value("limit").(int)
		format := c.Value("format").(string)
		noSnippet := c.Value("no-snippet").(bool)

		terms := strings.Join(c.Args().Slice(), " ")

		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		idx, err := index.OpenReadOnly(cfg.IndexPath())
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()

		req := query.Request{
			Terms:     terms,
			Fields:    fields,
			Limit:     limit,
			NoSnippet: noSnippet,
			Boosts:    cfg.Boosts,
		}
		switch format {
		case "json":
			req.Highlight = "html"
		default:
			req.Highlight = "ansi"
		}

		res, err := query.Search(idx, req)
		if err != nil {
			return err
		}

		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(res)
		}
		return query.RenderText(os.Stdout, res)
	},
}
