package halotrcer

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rajasatyajit/halotrcer/internal/report"
)

// userDocs are the documents a user reads.
func userDocs(t *testing.T) []string {
	t.Helper()
	docs, err := filepath.Glob("docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	return append(docs, "README.md", "SECURITY.md")
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	mdHeading = regexp.MustCompile(`(?m)^#{1,6} +(.+)$`)
	slugDrop  = regexp.MustCompile(`[^a-z0-9 _-]`)
)

// anchors are a document's heading anchors, as GitHub makes them.
func anchors(doc string) map[string]bool {
	out := map[string]bool{}
	for _, m := range mdHeading.FindAllStringSubmatch(doc, -1) {
		out[strings.ReplaceAll(slugDrop.ReplaceAllString(strings.ToLower(m[1]), ""), " ", "-")] = true
	}
	return out
}

// TestDocsLinksResolve: every relative link names a file that exists, and
// every #anchor a heading in it.
func TestDocsLinksResolve(t *testing.T) {
	for _, doc := range userDocs(t) {
		for _, m := range mdLink.FindAllStringSubmatch(readDoc(t, doc), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			file, anchor, _ := strings.Cut(target, "#")
			path := doc
			if file != "" {
				path = filepath.Join(filepath.Dir(doc), file)
				if _, err := os.Stat(path); err != nil {
					t.Errorf("%s links to %s, which does not exist", doc, m[1])
					continue
				}
			}
			if anchor != "" && strings.HasSuffix(path, ".md") && !anchors(readDoc(t, path))[anchor] {
				t.Errorf("%s links to %s, but %s has no such heading", doc, m[1], path)
			}
		}
	}
}

// TestDocsQuoteTheProduct: the privacy statement, the booking link and the
// install commands are the product's own constants, word for word.
func TestDocsQuoteTheProduct(t *testing.T) {
	for doc, want := range map[string][]string{
		"README.md":              {report.PrivacyStatement, report.CTAURL, report.CTAText, "https://www.halotrcer.com/install.sh", "https://www.halotrcer.com/install.ps1"},
		"SECURITY.md":            {report.PrivacyStatement, "satyajit.rajaraman@halotrcer.com", "5 business days"},
		"docs/faq.md":            {report.PrivacyStatement, report.CTAURL},
		"docs/report-anatomy.md": {report.PrivacyStatement, report.CTAURL, report.SchemaVersion},
	} {
		text := strings.Join(strings.Fields(readDoc(t, doc)), " ")
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s does not quote %q", doc, w)
			}
		}
	}
}

// TestREADMEIsShort: the brief's limit.
func TestREADMEIsShort(t *testing.T) {
	if n := strings.Count(readDoc(t, "README.md"), "\n"); n >= 250 {
		t.Errorf("README.md has %d lines; keep it under 250", n)
	}
}

var (
	commandUse = regexp.MustCompile(`halotrcer (check|install|doctor|update|uninstall)\b([^|\n` + "`" + `]*)`)
	flagRE     = regexp.MustCompile(`(?:^|\s)(--[a-z][a-z-]*)`)
	tableFlag  = regexp.MustCompile("(?m)^\\| `(--[a-z][a-z-]*)")
)

// TestDocsFlagsExist: every flag the docs give a halotrcer command, and
// every flag check.md's tables list, is one that command's -h prints.
func TestDocsFlagsExist(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "halotrcer")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "./cmd/halotrcer")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	help := map[string]string{}
	usage := func(cmd string) string {
		if h, ok := help[cmd]; ok {
			return h
		}
		out, err := exec.CommandContext(t.Context(), bin, cmd, "-h").CombinedOutput()
		if err != nil {
			t.Fatalf("halotrcer %s -h: %v\n%s", cmd, err, out)
		}
		help[cmd] = string(out)
		return help[cmd]
	}
	has := func(cmd, flag string) bool {
		return regexp.MustCompile(`(^|[\s\[(])` + regexp.QuoteMeta(flag) + `([\s\]),]|$)`).MatchString(usage(cmd))
	}
	checked := 0
	for _, doc := range userDocs(t) {
		text := readDoc(t, doc)
		for _, m := range commandUse.FindAllStringSubmatch(text, -1) {
			for _, f := range flagRE.FindAllStringSubmatch(m[2], -1) {
				checked++
				if !has(m[1], f[1]) {
					t.Errorf("%s: `halotrcer %s %s`, but halotrcer %s -h has no %s", doc, m[1], f[1], m[1], f[1])
				}
			}
		}
	}
	for _, m := range tableFlag.FindAllStringSubmatch(readDoc(t, "docs/check.md"), -1) {
		checked++
		if !has("check", m[1]) {
			t.Errorf("docs/check.md lists %s, but halotrcer check -h has no such flag", m[1])
		}
	}
	if checked < 20 {
		t.Errorf("only %d flags checked; the patterns no longer match the docs", checked)
	}
}
