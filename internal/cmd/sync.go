package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/sync"
	"github.com/urfave/cli/v3"
)

// Sync reconciles the codesearch index with the filesystem.
var Sync = &cli.Command{
	Name:  "sync",
	Usage: "Reconcile the codesearch index with the filesystem.",
	Action: func(ctx context.Context, c *cli.Command) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		res, err := sync.Run(ctx, cfg)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stdout, "sync complete: scanned=%d upserted=%d touched=%d deleted=%d unchanged=%d\n",
			res.Scanned, res.Upserted, res.Touched, res.Deleted, res.Unchanged)
		return nil
	},
}
