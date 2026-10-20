package report

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/ingest"
	"github.com/rajasatyajit/halotrcer/internal/redact"
	"github.com/rajasatyajit/halotrcer/internal/scoring"
)

// Inputs are everything a report is built from.
type Inputs struct {
	Generator string // "halotrcer vX.Y.Z"
	Now       time.Time
	Target    string // as the user gave it; redacted here

	Tokvera *ingest.TokveraReport
	Sievant *ingest.SievantScan
	Gateway *ingest.SievantGateway

	// Tools in the order they are shown: tokvera, sievant.
	Tools []ToolRun

	MonthlyRequests int64
	NoCTA           bool
	Redactor        *redact.Redactor
}

var weekTitles = map[int]string{
	1: "Week 1: critical exposure and quick fixes",
	2: "Week 2: the largest cost and latency failures",
	3: "Week 3: remaining high-severity security findings",
	4: "Week 4: everything else",
}

// Build assembles the combined report.
func Build(in Inputs) *Report {
	res := scoring.Score(in.Tokvera, in.Sievant)
	r := &Report{
		SchemaVersion: SchemaVersion,
		Generator:     in.Generator,
		GeneratedAt:   in.Now.UTC().Truncate(time.Second),
		Target:        in.Target,
		Tools:         append([]ToolRun{}, in.Tools...),
		Ranking:       Ranking{ScoringVersion: res.ScoringVersion, Provisional: res.Provisional, Note: RankingNote},
		Privacy:       PrivacyStatement,
	}
	if !in.NoCTA {
		r.CTA = &CTA{Text: CTAText, URL: CTAURL}
	}
	spend := scoring.MonthlySpend(in.Tokvera, in.MonthlyRequests)

	r.Summary = Summary{
		Top:                res.Top(5),
		DealRisk:           0,
		ReliabilityConcern: res.ReliabilityConcern,
		Spend:              spend,
	}
	for _, it := range res.Items {
		if it.DealRisk {
			r.Summary.DealRisk++
		}
	}
	for _, t := range r.Tools {
		if t.Status != "ok" {
			r.Summary.Partial = true
		}
	}

	var tokItems, svItems []scoring.Item
	for _, it := range res.Items {
		if it.Source == "tokvera" {
			tokItems = append(tokItems, it)
		} else {
			svItems = append(svItems, it)
		}
	}
	if in.Tokvera != nil {
		r.CostPerformance = costPerformance(in.Tokvera, tokItems, spend)
		r.Summary.Grade = in.Tokvera.Score.Grade
		r.Summary.GradeProvisional = in.Tokvera.Score.Provisional
		r.Summary.USDPer1kRequests = in.Tokvera.Cost.USDPer1kRequests
	}
	if in.Sievant != nil {
		r.AgentSecurity = agentSecurity(in.Sievant, svItems, in.Gateway)
		r.Summary.SecurityCounts = copyCounts(in.Sievant.Counts)
	}
	for w := 1; w <= 4; w++ {
		pw := PlanWeek{Week: w, Title: weekTitles[w], Items: []PlanItem{}}
		for _, it := range res.Week(w) {
			pw.Items = append(pw.Items, PlanItem{Source: it.Source, Ref: it.Ref, Title: it.Title, Fix: it.Fix, Effort: it.Effort, Where: where(it)})
		}
		r.FixPlan = append(r.FixPlan, pw)
	}
	if r.Summary.Top == nil {
		r.Summary.Top = []scoring.Item{}
	}
	redactAll(reflect.ValueOf(r).Elem(), in.Redactor)
	if r.CTA != nil { // a fixed, credential-free URL: never altered
		r.CTA.URL = CTAURL
	}
	return r
}

func where(it scoring.Item) string {
	var parts []string
	for _, p := range []string{it.File, it.Server, it.Where} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func copyCounts(m map[string]int) map[string]int {
	out := map[string]int{}
	for _, k := range []string{"critical", "high", "medium", "low", "info"} {
		out[k] = m[k]
	}
	return out
}

func costPerformance(t *ingest.TokveraReport, items []scoring.Item, spend *scoring.Spend) *CostPerformance {
	target := t.Target.BaseURL
	if target == "" {
		target = t.Target.URL
	}
	c := &CostPerformance{
		Target: target, Model: t.Model,
		Simulated: t.Simulated, SimulatedTiming: t.SimulatedTiming,
		Grade: t.Score.Grade, Points: t.Score.Points, GradeReason: t.Score.Reason,
		ScoringVersion: t.Score.ScoringVersion, Provisional: t.Score.Provisional,
		Requests: t.Run.Requests, Succeeded: t.Run.Succeeded, Failed: t.Run.Failed, DurationMS: t.Run.DurationMS,
		Spend: spend, Findings: items, Levels: []Level{},
		Cost: Cost{
			USDPer1kRequests: t.Cost.USDPer1kRequests, Estimate: t.Cost.Estimate, Basis: t.Cost.Basis,
			PricingSheet: t.Pricing.Sheet, PricingAsOf: t.Cost.PricingAsOf, PricingSource: t.Cost.PricingSource,
			PricingStale: t.Pricing.Stale, PricingWarning: t.Pricing.Warning,
		},
	}
	if c.Cost.PricingAsOf == "" {
		c.Cost.PricingAsOf = t.Pricing.AsOf
	}
	if c.Cost.PricingSource == "" {
		c.Cost.PricingSource = t.Pricing.Source
	}
	if t.Cost.USDPer1kRequests == nil {
		c.Cost.NotComputed = t.Cost.Note
		if c.Cost.NotComputed == "" {
			c.Cost.NotComputed = "not computed"
		}
	}
	if t.Score.Provisional {
		c.ProvisionalLabel, c.ProvisionalNote = t.Score.ProvisionalLabel, t.Score.ProvisionalNote
		if c.ProvisionalLabel == "" {
			c.ProvisionalLabel = TokveraProvisionalLabel
		}
		if c.ProvisionalNote == "" {
			c.ProvisionalNote = TokveraProvisionalNote
		}
	}
	for _, cp := range t.Score.CapsApplied {
		c.Caps = append(c.Caps, fmt.Sprintf("capped at %s: %s", cp.MaxGrade, cp.Reason))
	}
	for _, l := range t.Levels {
		c.Levels = append(c.Levels, Level{
			Concurrency: l.Concurrency, Requests: l.Requests, Failed: l.Failed,
			TTFTP50MS: l.TTFTMS.P50, TTFTP95MS: l.TTFTMS.P95, TTFTP99MS: l.TTFTMS.P99,
			DurationP95MS: l.RequestDurationMS.P95, TokensPerSP50: l.OutputTokensPerS.P50,
		})
	}
	for _, p := range []string{t.Privacy.Statement, t.Privacy.Prompts} {
		if p != "" {
			c.Privacy = append(c.Privacy, p)
		}
	}
	if c.Findings == nil {
		c.Findings = []scoring.Item{}
	}
	return c
}

func agentSecurity(s *ingest.SievantScan, items []scoring.Item, gw *ingest.SievantGateway) *AgentSecurity {
	a := &AgentSecurity{Counts: copyCounts(s.Counts), ToolsInspected: s.ToolsInspected, Findings: items, Notes: s.Notes, Files: []ConfigFile{}}
	for _, f := range s.Files {
		cf := ConfigFile{Client: f.Client, Path: f.Path, Scope: f.Scope, Servers: len(f.Servers), Error: f.Error}
		for _, sv := range f.Servers {
			if !sv.Wrapped && !sv.Disabled {
				cf.Unwrapped++
			}
		}
		a.Files = append(a.Files, cf)
	}
	if a.Findings == nil {
		a.Findings = []scoring.Item{}
	}
	if gw != nil {
		g := &Gateway{
			Period: gw.Period.Label, Coverage: gw.Summary.Coverage.Headline,
			ControlsEnforced: gw.Summary.ControlsEnforced, ControlsTotal: gw.Summary.ControlsTotal,
			Requests: gw.Summary.Requests, Blocked: gw.Summary.Blocked, Disclaimer: gw.Disclaimer, Gaps: []GapRow{},
		}
		if g.Coverage == "" {
			g.Coverage = gw.Summary.Coverage.State
		}
		order := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}
		gaps := append([]ingest.SievantGap(nil), gw.Gaps...)
		sort.SliceStable(gaps, func(i, j int) bool { return order[gaps[i].Severity] < order[gaps[j].Severity] })
		for _, gp := range gaps {
			g.Gaps = append(g.Gaps, GapRow{Severity: gp.Severity, Title: gp.Title, Fix: gp.Fix})
		}
		a.Gateway = g
	}
	return a
}

// redactAll scrubs every string reachable from v in place.
func redactAll(v reflect.Value, r *redact.Redactor) {
	if r == nil {
		return
	}
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(r.String(v.String()))
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			redactAll(v.Elem(), r)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				redactAll(v.Field(i), r)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			redactAll(v.Index(i), r)
		}
	case reflect.Map:
		if v.Type().Elem().Kind() == reflect.String {
			for _, k := range v.MapKeys() {
				v.SetMapIndex(k, reflect.ValueOf(r.String(v.MapIndex(k).String())))
			}
		}
	}
}
