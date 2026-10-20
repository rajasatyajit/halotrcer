package pdf

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestMetricsSpotCheck(t *testing.T) {
	// Values straight from Helvetica.afm and Helvetica-Bold.afm.
	cases := []struct {
		c    byte
		bold bool
		w    uint16
	}{{' ', false, 278}, {'A', false, 667}, {'W', false, 944}, {'i', false, 222}, {'A', true, 722}, {'i', true, 278}, {0x96, false, 556}, {0x80, false, 556}}
	for _, c := range cases {
		tbl := helveticaWidths
		if c.bold {
			tbl = helveticaBoldWidths
		}
		if got := tbl[c.c-32]; got != c.w {
			t.Errorf("width(%q, bold=%v) = %d, want %d", c.c, c.bold, got, c.w)
		}
	}
	if got := Width("AAA", 10, false); got != 20.01 {
		t.Errorf("Width = %v", got)
	}
}

func TestEncode(t *testing.T) {
	for in, want := range map[string]string{
		"plain":    "plain",
		"1–3 days": "1\x963 days",
		"€5 ×2 é":  "\x805 \xd72 \xe9",
		"≥ 20":     ">= 20",
		"日本":       "??",
		"a\tb\nc":  "a bc",
	} {
		if got := string(Encode(in)); got != want {
			t.Errorf("Encode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := escape([]byte(`a(b)\c` + "\xe9")); got != `a\(b\)\\c\351` {
		t.Errorf("escape = %q", got)
	}
}

func TestWrap(t *testing.T) {
	lines := Wrap("the quick brown fox jumps over the lazy dog", 10, false, 60)
	for _, l := range lines {
		if Width(l, 10, false) > 60 {
			t.Errorf("line too wide: %q", l)
		}
	}
	if strings.Join(lines, " ") != "the quick brown fox jumps over the lazy dog" {
		t.Errorf("wrap lost words: %q", lines)
	}
	long := Wrap(strings.Repeat("x", 200), 10, false, 50)
	if len(long) < 2 {
		t.Errorf("long word not broken: %q", long)
	}
	for _, l := range long {
		if l == "" || Width(l, 10, false) > 50 {
			t.Errorf("bad piece %q", l)
		}
	}
	if got := Wrap("a\n\nb", 10, false, 100); len(got) != 3 || got[1] != "" {
		t.Errorf("paragraphs: %q", got)
	}
}

func sample() []byte {
	d := New("Sample (report)")
	p := d.AddPage()
	p.Rect(40, 780, 515, 30, Color{0.1, 0.2, 0.3})
	p.Text(50, 790, 14, true, Color{1, 1, 1}, "Halotrcer – sample (1)")
	p.Line(40, 770, 555, 770, 0.5, Black)
	p.Text(50, 750, 10, false, Black, `Back\slash and €`)
	p.Link(50, 730, 100, 12, "https://www.halotrcer.com/book")
	d.AddPage().Text(50, 800, 10, false, Black, "page two")
	var b bytes.Buffer
	if _, err := d.WriteTo(&b); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func TestStructure(t *testing.T) {
	b := sample()
	if !bytes.HasPrefix(b, []byte("%PDF-1.4\n")) || !bytes.HasSuffix(b, []byte("%%EOF\n")) {
		t.Fatal("header or trailer")
	}
	if !bytes.Equal(b, sample()) {
		t.Error("not deterministic")
	}
	// Every xref offset points at its "N 0 obj".
	m := regexp.MustCompile(`startxref\n(\d+)\n`).FindSubmatch(b)
	xref, _ := strconv.Atoi(string(m[1]))
	lines := strings.Split(string(b[xref:]), "\n")
	for i, l := range lines[3:] {
		if !strings.HasSuffix(l, " n ") {
			break
		}
		off, _ := strconv.Atoi(l[:10])
		want := strconv.Itoa(i+1) + " 0 obj"
		if !bytes.HasPrefix(b[off:], []byte(want)) {
			t.Errorf("xref %d -> %q", i+1, b[off:off+12])
		}
	}
	for _, s := range []string{"/Count 2", "/URI (https://www.halotrcer.com/book)", "/BaseFont /Helvetica-Bold", `(Back\\slash and \200)`} {
		if !bytes.Contains(b, []byte(s)) {
			t.Errorf("missing %s", s)
		}
	}
}

// TestWithTools cross-checks the output with qpdf and pdftotext when they
// are installed (CI installs both).
func TestWithTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.pdf")
	if err := os.WriteFile(path, sample(), 0o600); err != nil {
		t.Fatal(err)
	}
	if q, err := exec.LookPath("qpdf"); err == nil {
		if out, err := exec.Command(q, "--check", path).CombinedOutput(); err != nil {
			t.Errorf("qpdf --check: %v\n%s", err, out)
		}
	} else if os.Getenv("CI") != "" {
		t.Fatal("qpdf not installed in CI")
	}
	if p, err := exec.LookPath("pdftotext"); err == nil {
		out, err := exec.Command(p, path, "-").Output()
		if err != nil {
			t.Fatalf("pdftotext: %v", err)
		}
		for _, s := range []string{"Halotrcer – sample (1)", `Back\slash and €`, "page two"} {
			if !strings.Contains(string(out), s) {
				t.Errorf("pdftotext lacks %q in %q", s, out)
			}
		}
	} else if os.Getenv("CI") != "" {
		t.Fatal("pdftotext not installed in CI")
	}
}
