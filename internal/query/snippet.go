package query

import (
	"fmt"
	"io"
	"strings"
)

// RenderText writes a terse, human-readable rendering of r to w. One hit
// per block: rank, score, path on the first line; title on the second;
// snippet on the third (if present).
func RenderText(w io.Writer, r *Result) error {
	if r == nil || len(r.Hits) == 0 {
		_, err := fmt.Fprintln(w, "no results")
		return err
	}

	for i, h := range r.Hits {
		title := h.Title
		if title == "" {
			title = h.Path
		}

		_, err := fmt.Fprintf(w, "%d. %s  (score %.2f)\n   %s\n", i+1, h.Path, h.Score, title)
		if err != nil {
			return err
		}

		snippet := strings.TrimSpace(h.Snippet)
		if snippet != "" {
			_, err = fmt.Fprintf(w, "   %s\n", snippet)
			if err != nil {
				return err
			}
		}

		if i < len(r.Hits)-1 {
			_, err = fmt.Fprintln(w)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
