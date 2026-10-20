package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Formats the report can be written in.
var Formats = []string{"html", "pdf", "json"}

// ParseFormats reads a --format list such as "html,pdf".
func ParseFormats(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.Split(s, ",") {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" || seen[f] {
			continue
		}
		ok := false
		for _, k := range Formats {
			ok = ok || f == k
		}
		if !ok {
			return nil, fmt.Errorf("unknown format %q (want html, pdf, json)", f)
		}
		seen[f] = true
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no format given (want html, pdf, json)")
	}
	return out, nil
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// Render writes one format.
func (r *Report) Render(format string, w io.Writer) error {
	switch format {
	case "html":
		return r.WriteHTML(w)
	case "pdf":
		return r.WritePDF(w)
	case "json":
		return r.WriteJSON(w)
	}
	return fmt.Errorf("unknown format %q", format)
}

// WriteFiles writes report.<format> for each format into dir (created with
// mode 0750), each atomically (temp file and rename) with mode 0600. It
// returns the paths written.
func (r *Report) WriteFiles(dir string, formats []string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	var paths []string
	for _, f := range formats {
		var buf bytes.Buffer
		if err := r.Render(f, &buf); err != nil {
			return paths, fmt.Errorf("render %s: %w", f, err)
		}
		p := filepath.Join(dir, "report."+f)
		if err := writeAtomic(p, buf.Bytes()); err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".halotrcer-report-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
