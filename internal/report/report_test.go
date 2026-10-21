package report

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/compat"
	"github.com/rajasatyajit/halotrcer/internal/ingest"
	"github.com/rajasatyajit/halotrcer/internal/redact"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	fixtures       = "../../testdata/fixtures"
	tokveraFixture = "tokvera-f2a7906"
)

var fixedNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func load(t *testing.T, tokFile, svFile, gwFile string) (*ingest.TokveraReport, *ingest.SievantScan, *ingest.SievantGateway) {
	t.Helper()
	m := compat.Load()
	var tok *ingest.TokveraReport
	var sv *ingest.SievantScan
	var gw *ingest.SievantGateway
	if tokFile != "" {
		// A bare name is in the current fixture; dir/name is another one.
		if !strings.Contains(tokFile, "/") {
			tokFile = filepath.Join(tokveraFixture, tokFile)
		}
		b, err := os.ReadFile(filepath.Join(fixtures, tokFile))
		if err != nil {
			t.Fatal(err)
		}
		if tok, err = ingest.DecodeTokvera(bytes.NewReader(b), m); err != nil {
			t.Fatal(err)
		}
	}
	if svFile != "" {
		b, err := os.ReadFile(filepath.Join(fixtures, "sievant-d8a8bcf", svFile))
		if err != nil {
			t.Fatal(err)
		}
		if sv, err = ingest.DecodeSievantScan(bytes.NewReader(b), m); err != nil {
			t.Fatal(err)
		}
	}
	if gwFile != "" {
		b, err := os.ReadFile(filepath.Join(fixtures, "sievant-d8a8bcf", gwFile))
		if err != nil {
			t.Fatal(err)
		}
		if gw, err = ingest.DecodeSievantGateway(bytes.NewReader(b), m); err != nil {
			t.Fatal(err)
		}
	}
	return tok, sv, gw
}

func fixtureRedactor() *redact.Redactor {
	return redact.New(redact.Options{Home: "/home/fixtureuser", Users: []string{"fixtureuser"}})
}

func inputs(tok *ingest.TokveraReport, sv *ingest.SievantScan, gw *ingest.SievantGateway) Inputs {
	return Inputs{
		Generator: "halotrcer test", Now: fixedNow, Target: "http://127.0.0.1:18089/v1",
		Tokvera: tok, Sievant: sv, Gateway: gw,
		Tools: []ToolRun{
			{Name: "tokvera", Version: "v0.1.0-dev", Status: "ok", CompatWarning: "tokvera v0.1.0-dev is outside the versions this release supports (>=0.2.0 <0.3.0)"},
			{Name: "sievant", Version: "v0.1.0-157-gd8a8bcf", Status: "ok"},
		},
		MonthlyRequests: 2_000_000,
		Redactor:        fixtureRedactor(),
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (run go test ./internal/report -update and review the diff)", name)
	}
}

func TestGolden(t *testing.T) {
	cases := []struct {
		name, tok, sv, gw string
		noCTA             bool
	}{
		{"sim-all", "sim.report.json", "scan-all.json", "", false},
		{"flaky-smoke-gateway", "flaky.report.json", "scan-smoke.json", "report.json", false},
		{"demo-no-cta", "demo.report.json", "scan-smoke.json", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok, sv, gw := load(t, c.tok, c.sv, c.gw)
			in := inputs(tok, sv, gw)
			in.NoCTA = c.noCTA
			r := Build(in)
			for _, f := range Formats {
				var b bytes.Buffer
				if err := r.Render(f, &b); err != nil {
					t.Fatal(err)
				}
				golden(t, c.name+"."+f, b.Bytes())
			}
		})
	}
}

func TestSections(t *testing.T) {
	tok, sv, _ := load(t, "flaky.report.json", "scan-all.json", "")
	r := Build(inputs(tok, sv, nil))
	if len(r.Summary.Top) != 5 {
		t.Fatalf("top %d", len(r.Summary.Top))
	}
	if r.Summary.ReliabilityConcern == "" || r.Summary.DealRisk == 0 {
		t.Errorf("summary %+v", r.Summary)
	}
	if r.CostPerformance.ProvisionalNote != TokveraProvisionalNote {
		t.Error("Tokvera's provisional note not carried")
	}
	if r.Summary.Spend == nil || r.Summary.Spend.MonthlyRequests != 2_000_000 {
		t.Errorf("spend %+v", r.Summary.Spend)
	}
	var html, pdfOut bytes.Buffer
	if err := r.WriteHTML(&html); err != nil {
		t.Fatal(err)
	}
	h := html.String()
	for _, want := range []string{"Executive summary", "Cost &amp; performance", "Agent security", "30-day fix plan", "Provisional scoring.", TokveraProvisionalNote, CTAURL, CTAText[:7], "reliability concern"} {
		if !strings.Contains(h, want) {
			t.Errorf("html lacks %q", want)
		}
	}
	// Self-contained: no script, no external resource.
	for _, re := range []string{`(?i)<script`, `(?i)<link[^>]+href`, `(?i)src=`, `(?i)url\(`, `(?i)@import`} {
		if regexp.MustCompile(re).MatchString(h) {
			t.Errorf("html matches %s", re)
		}
	}
	if !strings.Contains(h, `content="default-src 'none'; style-src 'unsafe-inline'"`) {
		t.Error("no CSP")
	}
	for _, s := range []string{"saving", "savings"} {
		for _, m := range regexp.MustCompile(`(?i)[^.]*\b`+s+`\b[^.]*`).FindAllString(h, -1) {
			if !strings.Contains(m, "not a saving") && !strings.Contains(m, "would save") {
				t.Errorf("savings claim in html: %q", m)
			}
		}
	}
	if err := r.WritePDF(&pdfOut); err != nil {
		t.Fatal(err)
	}
	r.CTA = nil
	html.Reset()
	if err := r.WriteHTML(&html); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.String(), "halotrcer.com/book") {
		t.Error("--no-cta left the CTA")
	}
}

func TestPartial(t *testing.T) {
	_, sv, _ := load(t, "", "scan-smoke.json", "")
	in := inputs(nil, sv, nil)
	in.Tools[0] = ToolRun{Name: "tokvera", Status: "failed", Error: "tokvera exited 1"}
	r := Build(in)
	if !r.Summary.Partial || r.CostPerformance != nil || r.AgentSecurity == nil {
		t.Errorf("%+v", r.Summary)
	}
	var b bytes.Buffer
	for _, f := range Formats {
		b.Reset()
		if err := r.Render(f, &b); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCanaries plants secrets, a home path and a user name in every input
// field the report shows and checks none reaches any format.
func TestCanaries(t *testing.T) {
	tok, sv, gw := load(t, "sim.report.json", "scan-smoke.json", "report.json")
	const (
		home   = "/home/canaryperson"
		user   = "canaryperson"
		secret = "sk-canary-0123456789abcdefgh" // gitleaks:allow (synthetic canary planted in every report field)
	)
	leakURL := "https://canaryperson:hunter22pass@api.example.com/v1?api_key=" + secret
	tok.Target.BaseURL = leakURL
	tok.Model = "model-of-" + user
	tok.Findings[0].Evidence = "seen at " + home + "/.config/x and " + leakURL
	tok.Findings[0].SuggestedFix = "export KEY=" + secret
	sv.Files[0].Path = home + "/.cursor/mcp.json"
	sv.Findings[0].File = home + "/.cursor/mcp.json"
	sv.Findings[0].Evidence = "Bearer " + secret
	sv.Findings[0].Detail = "user " + user + " wrote it"
	sv.Notes = append(sv.Notes, "note from "+home)
	gw.Gaps[0].Fix = "edit " + home + "/.config/sievant/config.yaml"
	in := inputs(tok, sv, gw)
	in.Target = leakURL
	in.Tools[0].Error = "failed reading " + home + "/x"
	in.Redactor = redact.New(redact.Options{Home: home, Users: []string{user}, Secrets: []string{secret}})
	r := Build(in)
	dir := t.TempDir()
	paths, err := r.WriteFiles(dir, Formats)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		if strings.HasSuffix(p, ".pdf") {
			if pt, err := exec.LookPath("pdftotext"); err == nil {
				out, err := exec.CommandContext(t.Context(), pt, p, "-").Output()
				if err != nil {
					t.Fatal(err)
				}
				text += string(out)
			}
		}
		if l := redact.Leaks(text, home, user, secret, "hunter22pass"); len(l) > 0 {
			t.Errorf("%s leaks %v", filepath.Base(p), l)
		}
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", p, fi.Mode())
		}
	}
}

func TestParseFormats(t *testing.T) {
	if f, err := ParseFormats("html, PDF,json,html"); err != nil || strings.Join(f, ",") != "html,pdf,json" {
		t.Errorf("%v %v", f, err)
	}
	for _, s := range []string{"", "docx", ","} {
		if _, err := ParseFormats(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestUSD(t *testing.T) {
	for in, want := range map[float64]string{0: "$0", 0.0475: "$0.0475", 0.047111111: "$0.0471", 95: "$95.00", 1234.5: "$1,234", 1e6: "$1,000,000"} {
		if got := usd(in); got != want {
			t.Errorf("usd(%v) = %q, want %q", in, got, want)
		}
	}
}

// TestTokveraProvisionalWords: from schema v1.3.0 Tokvera's report.json
// carries the label and note, and the combined report shows those words in
// every format; an older v1.x report (score.provisional only, the trimmed
// v1.2.0 fixture) gets the documented copy; a grade that is not provisional
// shows neither.
func TestTokveraProvisionalWords(t *testing.T) {
	tok, sv, _ := load(t, "flaky.report.json", "scan-all.json", "")
	if tok.SchemaVersion != "tokvera.scan-report/v1.3.0" || tok.Score.ProvisionalLabel == "" || tok.Score.ProvisionalNote == "" {
		t.Fatalf("the current fixture lacks the v1.3.0 label and note: %s %+v", tok.SchemaVersion, tok.Score)
	}
	// Distinct words, so the test sees whose words reach the output.
	newer := *tok
	newer.Score.ProvisionalLabel = "Label from report.json"
	newer.Score.ProvisionalNote = "Note from report.json."
	r := Build(inputs(&newer, sv, nil))
	if c := r.CostPerformance; c.ProvisionalLabel != "Label from report.json" || c.ProvisionalNote != "Note from report.json." {
		t.Errorf("v1.3.0: label %q note %q", c.ProvisionalLabel, c.ProvisionalNote)
	}
	var html, js bytes.Buffer
	if err := r.WriteHTML(&html); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteJSON(&js); err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"html": html.String(), "json": js.String()} {
		if !strings.Contains(out, "Label from report.json") || !strings.Contains(out, "Note from report.json.") {
			t.Errorf("%s does not show the report's own words", name)
		}
		if strings.Contains(out, TokveraProvisionalNote) {
			t.Errorf("%s shows the fallback note although the report carries one", name)
		}
	}

	if r := Build(inputs(tok, sv, nil)); r.CostPerformance.ProvisionalLabel != tok.Score.ProvisionalLabel || r.CostPerformance.ProvisionalNote != tok.Score.ProvisionalNote {
		t.Errorf("v1.3.0 fixture: the report's own words are not carried: %+v", r.CostPerformance)
	}

	old, _, _ := load(t, "tokvera-v1.2.0/flaky.report.json", "", "")
	if old.SchemaVersion != "tokvera.scan-report/v1.2.0" || !old.Score.Provisional || old.Score.ProvisionalLabel != "" {
		t.Fatalf("the v1.2.0 fixture is not an older provisional report: %s %+v", old.SchemaVersion, old.Score)
	}
	r = Build(inputs(old, sv, nil))
	if c := r.CostPerformance; c.ProvisionalLabel != TokveraProvisionalLabel || c.ProvisionalNote != TokveraProvisionalNote {
		t.Errorf("v1.2.0 fallback: label %q note %q", c.ProvisionalLabel, c.ProvisionalNote)
	}

	final := *tok
	final.Score.Provisional = false
	final.Score.ProvisionalLabel, final.Score.ProvisionalNote = "", ""
	r = Build(inputs(&final, sv, nil))
	html.Reset()
	if err := r.WriteHTML(&html); err != nil {
		t.Fatal(err)
	}
	if c := r.CostPerformance; c.ProvisionalLabel != "" || c.ProvisionalNote != "" || strings.Contains(html.String(), TokveraProvisionalLabel) {
		t.Errorf("a final grade shows a provisional label: %+v", c)
	}
}
