package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/ignore"
	syncpkg "github.com/mjpitz/codesearch/internal/sync"
	"github.com/mjpitz/codesearch/internal/watch"
	"github.com/urfave/cli/v3"
)

// Watch provides real-time synchronization between the filesystem and the codesearch index.
var Watch = &cli.Command{
	Name:  "watch",
	Usage: "Monitor directories for changes and automatically trigger re-indexing.",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "config",
			Usage: "Path to the configuration file.",
		},
		&cli.StringSliceFlag{
			Name:  "ignore",
			Usage: "Additional glob pattern(s) to ignore.",
		},
		&cli.DurationFlag{
			Name:  "debounce",
			Usage: "Time to wait after the last filesystem event before triggering a re-index.",
			Value: 500 * time.Millisecond,
		},
		&cli.BoolFlag{
			Name:  "verbose",
			Usage: "Enable verbose logging.",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		cfgPath := c.String("config")
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}

		// Handle additional ignore patterns
		extraIgnores := c.StringSlice("ignore")

		// For verbose logging, we'll use os.Stderr
		var logger *log.Logger
		if c.Bool("verbose") {
			logger = log.New(os.Stderr, "[watch] ", log.LstdFlags)
		} else {
			logger = log.New(os.Stderr, "", 0)
		}

		matcher, err := ignore.Load(cfg.Root)
		if err != nil {
			return fmt.Errorf("load ignore: %w", err)
		}

		// If extra ignores were provided, we might need to handle them.
		// However, the current ignore.Matcher implementation doesn't easily support
		// adding more patterns after loading. For now, we'll just note it.
		// In a real implementation, we'd probably want to extend the Matcher.
		if len(extraIgnores) > 0 {
			return fmt.Errorf("additional ignore patterns via --ignore are not yet supported")
		}

		w, err := watch.NewWatcher(cfg, matcher, c.Duration("debounce"), logger, syncpkg.Run)
		if err != nil {
			return err
		}
		defer w.Close()

		// Collect paths to watch. If none provided, use cfg.Root.
		// The user can provide multiple paths.
		args := c.Args().Slice()
		if len(args) == 0 {
			args = []string{cfg.Root}
		}

		if c.Bool("verbose") {
			logger.Printf("starting watch on %v (debounce: %v)\n", args, c.Duration("debounce"))
		}

		if err := w.Run(ctx, args); err != nil {
			return err
		}

		return nil
	},
}
