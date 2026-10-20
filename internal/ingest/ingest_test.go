package ingest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rajasatyajit/halotrcer/internal/compat"
)

const (
	tokveraDir = "../../testdata/fixtures/tokvera-b97e278"
	sievantDir = "../../testdata/fixtures/sievant-d8a8bcf"
)

func open(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestDecodeTokveraFixtures(t *testing.T) {
	m := compat.Load()
	cases := []struct {
		file      string
		grade     string
		findings  int
		cost      bool
		simulated bool
	}{
		{"demo.report.json", "D", 4, false, true},
		{"sim.report.json", "A", 1, true, false},
		{"flaky.report.json", "F", 3, true, false},
	}
	for _, c := range cases {
		rep, err := DecodeTokvera(open(t, filepath.Join(tokveraDir, c.file)), m)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if rep.Score.Grade != c.grade || len(rep.Findings) != c.findings || (rep.Cost.USDPer1kRequests != nil) != c.cost || rep.Simulated != c.simulated {
			t.Errorf("%s: grade %s findings %d cost %v simulated %v", c.file, rep.Score.Grade, len(rep.Findings), rep.Cost.USDPer1kRequests, rep.Simulated)
		}
		if !rep.Score.Provisional || rep.Score.ScoringVersion == "" {
			t.Errorf("%s: provisional %v scoring %q", c.file, rep.Score.Provisional, rep.Score.ScoringVersion)
		}
		for _, f := range rep.Findings {
			if f.CheckID == "" || (f.Status != "warn" && f.Status != "fail") {
				t.Errorf("%s: finding %+v", c.file, f)
			}
		}
		pillars := map[string]bool{}
		for _, ch := range rep.Checks {
			pillars[ch.Pillar] = true
		}
		if len(pillars) == 0 {
			t.Errorf("%s: no check pillars", c.file)
		}
	}
}

func TestDecodeSievantFixtures(t *testing.T) {
	m := compat.Load()
	rep, err := DecodeSievantScan(open(t, filepath.Join(sievantDir, "scan-all.json")), m)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Files) != 5 || rep.Counts["critical"] != 4 || len(rep.Findings) == 0 {
		t.Errorf("files %d counts %v findings %d", len(rep.Files), rep.Counts, len(rep.Findings))
	}
	for _, f := range rep.Findings {
		if f.Rule == "" || f.Severity == "" {
			t.Errorf("finding %+v", f)
		}
		if strings.Contains(f.Evidence, "FAKE") {
			t.Errorf("unmasked evidence in fixture: %s", f.Rule)
		}
	}
	gw, err := DecodeSievantGateway(open(t, filepath.Join(sievantDir, "report.json")), m)
	if err != nil {
		t.Fatal(err)
	}
	if gw.Summary.ControlsTotal == 0 || len(gw.Gaps) == 0 || gw.Disclaimer == "" {
		t.Errorf("gateway report: %+v", gw.Summary)
	}
}

func TestDecodeDoctorFixtures(t *testing.T) {
	for _, p := range []string{tokveraDir + "/doctor.json", sievantDir + "/doctor.json"} {
		d, err := DecodeDoctor(open(t, p))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if len(d.Checks) == 0 {
			t.Errorf("%s: no checks", p)
		}
	}
}

func TestDecodeRefusals(t *testing.T) {
	m := compat.Load()
	tok := []string{
		``,
		`not json`,
		`{"schema_version":"tokvera.scan-report/v2.0.0"}`,
		`{"schema_version":"tokvera.findings/v1.6.0"}`,
		`{}`,
	}
	for _, s := range tok {
		if _, err := DecodeTokvera(strings.NewReader(s), m); err == nil {
			t.Errorf("tokvera %q accepted", s)
		}
	}
	for _, s := range []string{``, `{}`, `{"report_version":2}`, `[]`} {
		if _, err := DecodeSievantScan(strings.NewReader(s), m); err == nil {
			t.Errorf("sievant %q accepted", s)
		}
	}
	if _, err := DecodeDoctor(strings.NewReader(`{"warnings":0}`)); err == nil {
		t.Error("doctor without checks accepted")
	}
	big := bytes.Repeat([]byte(" "), MaxDocument+2)
	if _, err := DecodeTokvera(bytes.NewReader(big), m); err == nil {
		t.Error("oversized document accepted")
	}
}

func TestUnknownFieldsIgnored(t *testing.T) {
	m := compat.Load()
	s := `{"schema_version":"tokvera.scan-report/v1.9.0","future_field":{"x":1},"score":{"grade":"B","provisional":true,"new":"y"}}`
	rep, err := DecodeTokvera(strings.NewReader(s), m)
	if err != nil || rep.Score.Grade != "B" {
		t.Fatalf("%v %+v", err, rep)
	}
	s = `{"report_version":1,"findings":[{"rule":"SEC001","severity":"critical","owasp":["LLM02"]}],"added":true}`
	sv, err := DecodeSievantScan(strings.NewReader(s), m)
	if err != nil || len(sv.Findings) != 1 {
		t.Fatalf("%v %+v", err, sv)
	}
}

func FuzzDecode(f *testing.F) {
	for _, p := range []string{tokveraDir + "/demo.report.json", sievantDir + "/scan-smoke.json", sievantDir + "/doctor.json"} {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	m := compat.Load()
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = DecodeTokvera(bytes.NewReader(b), m)
		_, _ = DecodeSievantScan(bytes.NewReader(b), m)
		_, _ = DecodeSievantGateway(bytes.NewReader(b), m)
		_, _ = DecodeDoctor(bytes.NewReader(b))
	})
}
