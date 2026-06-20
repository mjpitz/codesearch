package doc

import (
	"path"
	"regexp"
	"strings"
	"time"
)

// Doc is the indexable representation of a single file.
type Doc struct {
	Path        string         // repo-relative, forward slashes
	Title       string         // frontmatter.title -> first H1 -> filename
	Tags        []string       // frontmatter.tags, normalized (lowercase, trimmed)
	Body        string         // file body sans frontmatter (or full file if no frontmatter)
	Frontmatter map[string]any // raw frontmatter (nil when absent)
	ModTime     time.Time      // filled by sync
	Size        int64          // filled by sync
	Digest      string         // git-blob SHA, filled by sync
}

// FromBytes builds a Doc from file contents. path is the repo-relative
// forward-slash path used both as the doc ID and as the fallback for title
// derivation.
func FromBytes(filePath string, content []byte) (*Doc, error) {
	fm, body, err := Split(content)
	if err != nil {
		return nil, err
	}
	bodyStr := string(body)
	return &Doc{
		Path:        filePath,
		Title:       deriveTitle(fm, bodyStr, filePath),
		Tags:        normalizeStringSlice(lookup(fm, "tags")),
		Body:        bodyStr,
		Frontmatter: fm,
	}, nil
}

// reservedFields are populated from named Doc fields and must not be
// overwritten by raw frontmatter keys with the same name.
var reservedFields = map[string]struct{}{
	"path":   {},
	"title":  {},
	"tags":   {},
	"body":   {},
	"mtime":  {},
	"size":   {},
	"digest": {},
}

// Flatten returns the field map used to populate a Bleve document.
// Frontmatter is merged in with one level of nesting flattened to dotted
// keys (e.g. `service.name`). Reserved Doc fields always win over raw
// frontmatter entries with the same key.
func (d *Doc) Flatten() map[string]any {
	out := map[string]any{
		"path":  d.Path,
		"title": d.Title,
		"tags":  d.Tags,
		"body":  d.Body,
	}
	if !d.ModTime.IsZero() {
		out["mtime"] = float64(d.ModTime.UnixNano())
	}
	if d.Size > 0 {
		out["size"] = float64(d.Size)
	}
	if d.Digest != "" {
		out["digest"] = d.Digest
	}

	for k, v := range d.Frontmatter {
		if _, taken := reservedFields[k]; taken {
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			for sk, sv := range t {
				out[k+"."+sk] = sv
			}
		default:
			out[k] = v
		}
	}
	return out
}

var h1Re = regexp.MustCompile(`(?m)^#\s+(.+)$`)

func deriveTitle(fm map[string]any, body, filePath string) string {
	if t, ok := lookup(fm, "title").(string); ok {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			return trimmed
		}
	}
	if m := h1Re.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(m[1])
	}
	name := path.Base(filePath)
	if ext := path.Ext(name); ext != "" {
		name = name[:len(name)-len(ext)]
	}
	return name
}

func lookup(fm map[string]any, key string) any {
	if fm == nil {
		return nil
	}
	return fm[key]
}

func normalizeStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	var out []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				if n := strings.ToLower(strings.TrimSpace(s)); n != "" {
					out = append(out, n)
				}
			}
		}
	case []string:
		for _, s := range t {
			if n := strings.ToLower(strings.TrimSpace(s)); n != "" {
				out = append(out, n)
			}
		}
	case string:
		if n := strings.ToLower(strings.TrimSpace(t)); n != "" {
			out = append(out, n)
		}
	}
	return out
}
