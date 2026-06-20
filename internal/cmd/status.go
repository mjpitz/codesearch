package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mjpitz/units/data"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/mjpitz/codesearch/internal/sync"
	"github.com/urfave/cli/v3"
)

// Status reports doc count, on-disk size, and last sync time.
var Status = &cli.Command{
	Name:  "status",
	Usage: "Report doc count, on-disk size, and last sync time.",
	Action: func(ctx context.Context, c *cli.Command) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}

		idx, err := index.OpenReadOnly(cfg.IndexPath())
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("no index found at %s; run `codesearch init` first", cfg.IndexPath())
			}
			return err
		}
		defer func() { _ = idx.Close() }()

		count, err := idx.DocCount()
		if err != nil {
			return err
		}
		meta, err := sync.ReadMeta(cfg.MetaPath())
		if err != nil {
			return err
		}
		size, err := dirSize(cfg.IndexPath())
		if err != nil {
			return err
		}

		lastSync := "never"
		if !meta.LastSyncAt.IsZero() {
			lastSync = meta.LastSyncAt.Format("2006-01-02 15:04:05 MST")
		}

		_, _ = fmt.Fprintf(os.Stdout, "index:          %s\n", cfg.IndexPath())
		_, _ = fmt.Fprintf(os.Stdout, "docs:           %d\n", count)
		_, _ = fmt.Fprintf(os.Stdout, "size:           %s\n", formatSize(size))
		_, _ = fmt.Fprintf(os.Stdout, "schema_version: %d\n", meta.SchemaVersion)
		_, _ = fmt.Fprintf(os.Stdout, "last_sync_at:   %s\n", lastSync)
		return nil
	},
}

// formatSize renders n as a single-unit IEC value rounded to 2 decimals
// (e.g. "2.55 MiB"). Bytes under 1 KiB are shown as an integer count.
func formatSize(n int64) string {
	s := data.Size(n)
	switch {
	case s >= data.Pebibyte:
		return fmt.Sprintf("%.2f PiB", s.As(data.Pebibyte))
	case s >= data.Tebibyte:
		return fmt.Sprintf("%.2f TiB", s.As(data.Tebibyte))
	case s >= data.Gibibyte:
		return fmt.Sprintf("%.2f GiB", s.As(data.Gibibyte))
	case s >= data.Mebibyte:
		return fmt.Sprintf("%.2f MiB", s.As(data.Mebibyte))
	case s >= data.Kibibyte:
		return fmt.Sprintf("%.2f KiB", s.As(data.Kibibyte))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		return nil
	})
	return total, err
}
