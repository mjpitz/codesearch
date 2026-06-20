// Package doc parses YAML frontmatter and represents the indexable
// document model that all downstream layers consume.
package doc

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

var (
	openDelim    = []byte("---\n")
	closeDelim   = []byte("\n---\n")
	closeNoTrail = []byte("\n---")
)

// Split extracts the YAML frontmatter block from a file body. The
// frontmatter must start at the very first byte with `---\n` and end with a
// line that is exactly `---`. When no frontmatter is detected (or the
// closing delimiter is missing), the returned frontmatter is nil and body
// equals the input content. A YAML parse error is returned as-is so callers
// can decide whether to fail the index or fall back to a raw body.
func Split(content []byte) (frontmatter map[string]any, body []byte, err error) {
	if !bytes.HasPrefix(content, openDelim) {
		return nil, content, nil
	}
	rest := content[len(openDelim):]

	var yamlBlock []byte
	switch {
	case bytes.Contains(rest, closeDelim):
		idx := bytes.Index(rest, closeDelim)
		yamlBlock = rest[:idx]
		body = rest[idx+len(closeDelim):]
	case bytes.HasSuffix(rest, closeNoTrail):
		yamlBlock = rest[:len(rest)-len(closeNoTrail)]
		body = nil
	default:
		return nil, content, nil
	}

	err = yaml.Unmarshal(yamlBlock, &frontmatter)
	if err != nil {
		return nil, content, err
	}

	return frontmatter, body, nil
}
