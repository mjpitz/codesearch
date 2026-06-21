package cmd

import (
	"context"
	"fmt"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/urfave/cli/v3"
)

var Fields = &cli.Command{
	Name:  "fields",
	Usage: "List every indexed field name in the codesearch index.",
	Action: func(ctx context.Context, c *cli.Command) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		idx, err := index.OpenReadOnly(cfg.IndexPath())
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()

		fields, err := idx.Bleve().Fields()
		if err != nil {
			return err
		}

		for _, field := range fields {
			fmt.Println(field)
		}

		return nil
	},
}
