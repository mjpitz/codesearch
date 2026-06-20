// Package cmd defines *cli.Command values for each codesearch subcommand.
// Each command is a thin wrapper that parses args (with flag.FlagSet where
// needed) and delegates to the relevant internal package.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/mjpitz/codesearch/internal/sync"
	"github.com/urfave/cli/v3"
)

// Init creates an empty codesearch index at .codesearch/.
var Init = &cli.Command{
	Name:  "init",
	Usage: "Create an empty codesearch index at .codesearch/.",
	Action: func(ctx context.Context, c *cli.Command) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}

		err = os.MkdirAll(cfg.IndexDir(), 0o755)
		if err != nil {
			return err
		}

		_, err = os.Stat(cfg.IndexPath())
		if err == nil {
			return fmt.Errorf("index already exists at %s", cfg.IndexPath())
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		idx, err := index.Create(cfg.IndexPath())
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()

		meta := sync.Meta{
			SchemaVersion: index.SchemaVersion,
			LastSyncAt:    time.Time{},
		}

		err = sync.WriteMeta(cfg.MetaPath(), meta)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(os.Stdout, "created index at %s\n", cfg.IndexPath())
		return nil
	},
}
