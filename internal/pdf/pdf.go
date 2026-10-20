// Package pdf writes small, deterministic PDF 1.4 documents with no
// dependency: the standard Type1 fonts Helvetica and Helvetica-Bold (not
// embedded) in WinAnsiEncoding, text measured with Adobe's AFM widths
// (metrics.go), uncompressed content streams, and no timestamps, so the same
// input gives the same bytes. Written for Halotrcer from the PDF 1.4
// specification; the technique matches Sievant's report PDF, the code is
// independent of it.
package pdf

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// A4 in points.
const (
	PageWidth  = 595.28
	PageHeight = 841.89
)

// Color is an RGB color, each component 0-1.
type Color struct{ R, G, B float64 }

// Black is the default text color.
var Black = Color{}

// Document is a PDF under construction.
type Document struct {
	pages []*Page
	title string
}

// Page is one page; coordinates are points from the bottom-left corner.
type Page struct {
	content bytes.Buffer
	links   []link
}

type link struct {
	x, y, w, h float64
	uri        string
}

// New starts a document; title goes in the Info dictionary.
func New(title string) *Document { return &Document{title: title} }

// AddPage appends an A4 page.
func (d *Document) AddPage() *Page {
	p := &Page{}
	d.pages = append(d.pages, p)
	return p
}

// Pages is the number of pages.
func (d *Document) Pages() int { return len(d.pages) }

// Text draws s with its baseline starting at (x, y).
func (p *Page) Text(x, y, size float64, bold bool, c Color, s string) {
	font := "F1"
	if bold {
		font = "F2"
	}
	fmt.Fprintf(&p.content, "BT %s rg /%s %s Tf %s %s Td (%s) Tj ET\n",
		rgb(c), font, num(size), num(x), num(y), escape(Encode(s)))
}

// Rect fills a rectangle.
func (p *Page) Rect(x, y, w, h float64, c Color) {
	fmt.Fprintf(&p.content, "%s rg %s %s %s %s re f\n", rgb(c), num(x), num(y), num(w), num(h))
}

// Line strokes a line.
func (p *Page) Line(x1, y1, x2, y2, width float64, c Color) {
	fmt.Fprintf(&p.content, "%s RG %s w %s %s m %s %s l S\n", rgb(c), num(width), num(x1), num(y1), num(x2), num(y2))
}

// Link makes a rectangle a link to uri.
func (p *Page) Link(x, y, w, h float64, uri string) {
	p.links = append(p.links, link{x, y, w, h, uri})
}

func rgb(c Color) string { return num(c.R) + " " + num(c.G) + " " + num(c.B) }

func num(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// transliterations for common characters outside WinAnsi, before the rest
// become '?'.
var transliterations = map[rune]string{
	'≥': ">=", '≤': "<=", '→': "->", '←': "<-", '≈': "~", '−': "-",
	'✓': "v", '✗': "x", ' ': " ", ' ': " ", ' ': " ",
}

// Encode converts s to WinAnsi bytes: characters WinAnsi lacks are
// transliterated where a close ASCII form exists, otherwise written as '?'.
// Control characters other than tab (written as a space) are dropped.
func Encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			out = append(out, ' ')
		case r < 32 || r == 127:
			continue
		case r < 128:
			out = append(out, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			if b, ok := highCode(r); ok {
				out = append(out, b)
			} else if t, ok := transliterations[r]; ok {
				out = append(out, t...)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

func highCode(r rune) (byte, bool) {
	for i, u := range cp1252High[:32] { // 0x80-0x9F: the only non-Latin-1 codes
		if u == r && u != 0 {
			return byte(0x80 + i), true
		}
	}
	return 0, false
}

func escape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == '(' || c == ')' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < 32 || c > 126:
			fmt.Fprintf(&sb, "\\%03o", c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// Width is the advance width of s in points.
func Width(s string, size float64, bold bool) float64 {
	tbl := &helveticaWidths
	if bold {
		tbl = &helveticaBoldWidths
	}
	var total int
	for _, c := range Encode(s) {
		if c >= 32 {
			total += int(tbl[c-32])
		}
	}
	return float64(total) * size / 1000
}

// Wrap breaks s into lines no wider than max points, at spaces; a word
// longer than a line is broken where it overflows.
func Wrap(s string, size float64, bold bool, max float64) []string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := ""
		for _, w := range words {
			for Width(w, size, bold) > max {
				if cur != "" {
					lines = append(lines, cur)
					cur = ""
				}
				cut := fit(w, size, bold, max)
				lines = append(lines, w[:cut])
				w = w[cut:]
			}
			cand := w
			if cur != "" {
				cand = cur + " " + w
			}
			if Width(cand, size, bold) <= max {
				cur = cand
			} else {
				lines = append(lines, cur)
				cur = w
			}
		}
		lines = append(lines, cur)
	}
	return lines
}

// fit returns the longest prefix length (at a rune boundary, at least one
// rune) of w that fits in max.
func fit(w string, size float64, bold bool, max float64) int {
	last := 0
	for i := range w {
		if i > 0 && Width(w[:i], size, bold) > max {
			break
		}
		last = i
	}
	if last == 0 {
		for i := range w {
			if i > 0 {
				return i
			}
		}
		return len(w)
	}
	return last
}

// WriteTo writes the document.
func (d *Document) WriteTo(w io.Writer) (int64, error) {
	if len(d.pages) == 0 {
		d.AddPage()
	}
	var buf bytes.Buffer
	var offsets []int
	obj := func(body string) int {
		offsets = append(offsets, buf.Len())
		n := len(offsets)
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// Fixed object numbers: 1 catalog, 2 pages, 3 and 4 fonts, 5 info;
	// then per page: page, content, and one object per link.
	nPages := len(d.pages)
	pageNums := make([]int, nPages)
	next := 6
	for i, p := range d.pages {
		pageNums[i] = next
		next += 2 + len(p.links)
	}
	kids := make([]string, nPages)
	for i, n := range pageNums {
		kids[i] = fmt.Sprintf("%d 0 R", n)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), nPages))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	obj(fmt.Sprintf("<< /Title (%s) /Producer (halotrcer) >>", escape(Encode(d.title))))
	for i, p := range d.pages {
		n := pageNums[i]
		var annots []string
		for j := range p.links {
			annots = append(annots, fmt.Sprintf("%d 0 R", n+2+j))
		}
		annot := ""
		if len(annots) > 0 {
			annot = " /Annots [" + strings.Join(annots, " ") + "]"
		}
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R%s >>",
			num(PageWidth), num(PageHeight), n+1, annot))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", p.content.Len(), p.content.String()))
		for _, l := range p.links {
			obj(fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [%s %s %s %s] /Border [0 0 0] /A << /S /URI /URI (%s) >> >>",
				num(l.x), num(l.y), num(l.x+l.w), num(l.y+l.h), escape(Encode(l.uri))))
		}
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 5 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	n, err := w.Write(buf.Bytes())
	return int64(n), err
}
