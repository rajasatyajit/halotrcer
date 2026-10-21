package compat

import (
	"strings"
	"testing"
)

func TestEmbeddedMatrix(t *testing.T) {
	m := Load()
	if m.MatrixVersion != 1 {
		t.Fatalf("matrix_version = %d", m.MatrixVersion)
	}
	for _, name := range []string{"tokvera", "sievant"} {
		tool, ok := m.Tools[name]
		if !ok {
			t.Fatalf("%s missing", name)
		}
		lo, err1 := ParseVersion(tool.Min)
		hi, err2 := ParseVersion(tool.Below)
		if err1 != nil || err2 != nil || coreCmp(lo, hi) >= 0 {
			t.Errorf("%s: bad range %q..%q", name, tool.Min, tool.Below)
		}
	}
	if tok := m.Tools["tokvera"]; m.CheckTokveraSchema(tok.SchemaTested) != nil || !strings.HasPrefix(tok.SchemaTested, tok.SchemaPrefix) {
		t.Errorf("tokvera: schema_tested %q is not a schema this release reads", tok.SchemaTested)
	}
}

func TestCheckRange(t *testing.T) {
	m := Load()
	cases := []struct {
		tool, v string
		ok      bool
	}{
		{"tokvera", "v0.2.0", true},
		{"tokvera", "v0.2.0-rc.1", true}, // rcs of a supported version are in range (D2)
		{"tokvera", "0.2.7", true},
		{"tokvera", "v0.1.0", false},
		{"tokvera", "v0.1.0-dev", false},
		{"tokvera", "v0.3.0-rc.1", false},
		{"sievant", "v0.2.9", true},
		{"sievant", "v1.0.0", false},
		{"sievant", "garbage", false},
		{"other", "v0.2.0", false},
	}
	for _, c := range cases {
		err := m.Check(c.tool, c.v)
		if (err == nil) != c.ok {
			t.Errorf("Check(%s, %s) = %v, want ok=%v", c.tool, c.v, err, c.ok)
		}
	}
}

func TestParseVersionLine(t *testing.T) {
	cases := []struct {
		tool, out, want string
	}{
		{"tokvera", "tokvera v0.2.0 (commit abc1234)\nbuilt with go1.27.2\n", "v0.2.0"},
		{"tokvera", "tokvera v0.1.0-dev (commit unknown)\n", "v0.1.0-dev"},
		{"sievant", "sievant v0.2.0-rc.1\n", "v0.2.0-rc.1"},
		{"sievant", "sievant dev\n", "dev"},
	}
	for _, c := range cases {
		got, err := ParseVersionLine(c.tool, c.out)
		if err != nil || got != c.want {
			t.Errorf("ParseVersionLine(%s, %q) = %q, %v; want %q", c.tool, c.out, got, err, c.want)
		}
	}
	if _, err := ParseVersionLine("sievant", "tokvera v0.2.0\n"); err == nil {
		t.Error("a tokvera line parsed as sievant")
	}
}

func TestSchemaGates(t *testing.T) {
	m := Load()
	for _, s := range []string{"tokvera.scan-report/v1.2.0", "tokvera.scan-report/v1.9.0"} {
		if err := m.CheckTokveraSchema(s); err != nil {
			t.Errorf("%s refused: %v", s, err)
		}
	}
	if err := m.CheckTokveraSchema("tokvera.scan-report/v2.0.0"); err == nil || !strings.Contains(err.Error(), "major") {
		t.Errorf("v2 not refused as an unknown major: %v", err)
	}
	if err := m.CheckTokveraSchema("tokvera.findings/v1.6.0"); err == nil {
		t.Error("an audit findings document accepted as a scan report")
	}
	if err := m.CheckSievantReport(1); err != nil {
		t.Error(err)
	}
	for _, v := range []int{0, 2} {
		if err := m.CheckSievantReport(v); err == nil {
			t.Errorf("report_version %d accepted", v)
		}
	}
}
