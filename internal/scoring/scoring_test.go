package scoring

import (
	"math"
	"os"
	"testing"

	"github.com/rajasatyajit/halotrcer/internal/compat"
	"github.com/rajasatyajit/halotrcer/internal/ingest"
)

func f64(v float64) *float64 { return &v }

func tok(findings []ingest.TokveraFinding, checks []ingest.TokveraCheck, grade string) *ingest.TokveraReport {
	return &ingest.TokveraReport{Findings: findings, Checks: checks, Score: ingest.TokveraScore{Grade: grade}}
}

func TestTiersAndOrder(t *testing.T) {
	checks := []ingest.TokveraCheck{
		{ID: "ttft", Pillar: "latency"}, {ID: "cache", Pillar: "cost_and_cache"},
		{ID: "metrics", Pillar: "observability"}, {ID: "errs", Pillar: "reliability"},
	}
	tr := tok([]ingest.TokveraFinding{
		{Rank: 1, CheckID: "ttft", Status: "fail", Impact: 35, Effort: "2–5 days"},
		{Rank: 2, CheckID: "errs", Status: "fail", Impact: 30, Effort: "0.5–3 days"},
		{Rank: 3, CheckID: "cache", Status: "fail", Impact: 10, Effort: "1–3 days"},
		{Rank: 4, CheckID: "metrics", Status: "warn", Impact: 5, Effort: "0.5 days"},
	}, checks, "D")
	sv := &ingest.SievantScan{Findings: []ingest.SievantFinding{
		{Rule: "SUP001", Severity: "medium"},
		{Rule: "PERM003", Severity: "high"},
		{Rule: "AUTH001", Severity: "high"},
		{Rule: "SEC001", Severity: "critical"},
		{Rule: "NET001", Severity: "medium"},
	}}
	res := Score(tr, sv)
	got := make([]string, 0, len(res.Items))
	for _, it := range res.Items {
		got = append(got, it.Ref)
	}
	// tier 1: SEC001 (critical, weight 1) then AUTH001 (deal risk, high);
	// tier 2: ttft, errs (fails on latency at/above median 20);
	// tier 3: PERM003 (high, no deal risk); tier 4: cache (fail below median);
	// tier 5: NET001 (medium, weight .5), metrics (warn, weight 5/35*.5), SUP001 (medium).
	want := []string{"SEC001", "AUTH001", "ttft", "errs", "PERM003", "cache", "NET001", "SUP001", "metrics"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if res.ReliabilityConcern == "" {
		t.Error("grade D: no reliability concern")
	}
	for _, it := range res.Items {
		if it.Source == "tokvera" && it.DealRisk {
			t.Errorf("tokvera item %s marked deal risk (D3: Sievant rules only)", it.Ref)
		}
		if it.Why == "" {
			t.Errorf("%s has no reason", it.Ref)
		}
	}
	nd := res.Items[6] // NET001, medium: deal-risk rule but not tier 1
	if !nd.DealRisk || nd.Tier != 5 {
		t.Errorf("NET001 medium: %+v", nd)
	}
	if len(res.Top(5)) != 5 || len(res.Top(50)) != 9 {
		t.Error("Top")
	}
	weeks := map[string]int{}
	for _, it := range res.Items {
		weeks[it.Ref] = it.Week
	}
	wantWeeks := map[string]int{"SEC001": 1, "AUTH001": 1, "ttft": 2, "errs": 2, "PERM003": 3, "cache": 4, "NET001": 4, "SUP001": 4, "metrics": 1}
	for k, w := range wantWeeks {
		if weeks[k] != w {
			t.Errorf("%s in week %d, want %d", k, weeks[k], w)
		}
	}
}

func TestNoGradeConcernForGoodGrades(t *testing.T) {
	for _, g := range []string{"A", "B", "C", "insufficient_evidence", ""} {
		if r := Score(tok(nil, nil, g), nil); r.ReliabilityConcern != "" {
			t.Errorf("grade %q: %q", g, r.ReliabilityConcern)
		}
	}
	if r := Score(nil, nil); len(r.Items) != 0 || !r.Provisional || r.ScoringVersion != Version {
		t.Errorf("empty: %+v", r)
	}
}

func TestDeterministic(t *testing.T) {
	sv := &ingest.SievantScan{Findings: []ingest.SievantFinding{
		{Rule: "SEC001", Severity: "critical", File: "b", Server: "x"},
		{Rule: "SEC001", Severity: "critical", File: "a", Server: "y"},
		{Rule: "SEC001", Severity: "critical", File: "a", Server: "x"},
	}}
	r := Score(nil, sv)
	if r.Items[0].File != "a" || r.Items[0].Server != "x" || r.Items[2].File != "b" {
		t.Errorf("tie-break: %+v", r.Items)
	}
}

func TestUnderADay(t *testing.T) {
	for in, want := range map[string]bool{
		"0.5 days": true, "0.5–3 days": false, "1–3 days": false, "2 hours": true,
		"4–10 hours": false, "": false, "unknown": false, "0.9 day": true,
	} {
		if got := underADay(in); got != want {
			t.Errorf("underADay(%q) = %v", in, got)
		}
	}
}

func TestMonthlySpend(t *testing.T) {
	r := &ingest.TokveraReport{Cost: ingest.TokveraCost{USDPer1kRequests: f64(0.0475), PricingAsOf: "2026-10-20", PricingSource: "src", Estimate: true}}
	s := MonthlySpend(r, 2_000_000)
	if s == nil || math.Abs(s.MonthlyUSD-95) > 1e-9 || s.PricingAsOf != "2026-10-20" || !s.Estimate {
		t.Errorf("%+v", s)
	}
	if MonthlySpend(r, 0) != nil {
		t.Error("spend without a volume")
	}
	if MonthlySpend(&ingest.TokveraReport{}, 1000) != nil {
		t.Error("spend without a measured cost")
	}
	if MonthlySpend(nil, 1000) != nil {
		t.Error("spend without a report")
	}
}

func TestFixtures(t *testing.T) {
	m := compat.Load()
	f, err := os.Open("../../testdata/fixtures/tokvera-f2a7906/demo.report.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	tr, err := ingest.DecodeTokvera(f, m)
	if err != nil {
		t.Fatal(err)
	}
	g, err := os.Open("../../testdata/fixtures/sievant-d8a8bcf/scan-smoke.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	sv, err := ingest.DecodeSievantScan(g, m)
	if err != nil {
		t.Fatal(err)
	}
	res := Score(tr, sv)
	if len(res.Items) != len(tr.Findings)+len(sv.Findings) {
		t.Fatalf("items %d", len(res.Items))
	}
	top := res.Top(5)
	if top[0].Tier != 1 || top[0].Source != "sievant" {
		t.Errorf("top item: %+v", top[0])
	}
	for i := 1; i < len(res.Items); i++ {
		if less(res.Items[i], res.Items[i-1]) {
			t.Fatalf("not sorted at %d", i)
		}
	}
}
