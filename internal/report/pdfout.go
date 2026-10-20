package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/rajasatyajit/halotrcer/internal/pdf"
	"github.com/rajasatyajit/halotrcer/internal/scoring"
)

type scoringItem = scoring.Item

const (
	marginX  = 48.0
	marginT  = 56.0
	marginB  = 56.0
	contentW = pdf.PageWidth - 2*marginX
)

var (
	colAccent = pdf.Color{R: 0.12, G: 0.37, B: 0.55}
	colMuted  = pdf.Color{R: 0.36, G: 0.40, B: 0.44}
	colLine   = pdf.Color{R: 0.85, G: 0.87, B: 0.89}
	colCrit   = pdf.Color{R: 0.64, G: 0.15, B: 0.16}
	colBanner = pdf.Color{R: 0.96, G: 0.95, B: 0.92}
)

// layout flows text down A4 pages.
type layout struct {
	doc  *pdf.Document
	page *pdf.Page
	y    float64
	n    int
}

func newLayout(title string) *layout {
	l := &layout{doc: pdf.New(title)}
	l.newPage()
	return l
}

func (l *layout) newPage() {
	l.page = l.doc.AddPage()
	l.n++
	l.y = pdf.PageHeight - marginT
	l.page.Text(marginX, marginB/2, 8, false, colMuted, "Halotrcer report · page "+strconv.Itoa(l.n))
}

func (l *layout) ensure(h float64) {
	if l.y-h < marginB {
		l.newPage()
	}
}

func (l *layout) space(h float64) { l.y -= h }

// text writes wrapped text at an indent.
func (l *layout) text(s string, size float64, bold bool, c pdf.Color, indent float64) {
	lead := size * 1.35
	for _, line := range pdf.Wrap(s, size, bold, contentW-indent) {
		l.ensure(lead)
		l.y -= lead
		l.page.Text(marginX+indent, l.y+size*0.25, size, bold, c, line)
	}
}

func (l *layout) heading(s string) {
	l.ensure(48)
	l.space(14)
	l.text(s, 15, true, colAccent, 0)
	l.space(3)
	l.page.Line(marginX, l.y, marginX+contentW, l.y, 0.6, colLine)
	l.space(6)
}

func (l *layout) subheading(s string) {
	l.ensure(30)
	l.space(6)
	l.text(s, 11.5, true, pdf.Black, 0)
	l.space(2)
}

func (l *layout) banner(s string) {
	lines := pdf.Wrap(s, 9.5, false, contentW-16)
	h := float64(len(lines))*9.5*1.35 + 8
	l.ensure(h + 4)
	l.page.Rect(marginX, l.y-h, contentW, h, colBanner)
	l.page.Rect(marginX, l.y-h, 3, h, colCrit)
	l.space(4)
	for _, line := range lines {
		l.y -= 9.5 * 1.35
		l.page.Text(marginX+10, l.y+2.4, 9.5, false, pdf.Black, line)
	}
	l.space(8)
}

// WritePDF writes the report as PDF (A4, standard Helvetica, no dependency).
func (r *Report) WritePDF(w io.Writer) error {
	l := newLayout("Halotrcer report")
	l.text("Halotrcer report", 20, true, pdf.Black, 0)
	meta := r.GeneratedAt.Format("2006-01-02 15:04 UTC") + " · " + r.Generator
	if r.Target != "" {
		meta = "Target " + r.Target + " · " + meta
	}
	l.text(meta, 9, false, colMuted, 0)
	var tools []string
	for _, t := range r.Tools {
		s := sourceName(t.Name)
		if t.Version != "" {
			s += " " + t.Version
		}
		tools = append(tools, s+" ("+t.Status+")")
	}
	l.text(strings.Join(tools, " · "), 9, false, colMuted, 0)
	for _, t := range r.Tools {
		if t.CompatWarning != "" {
			l.banner(t.CompatWarning)
		}
		if t.Error != "" {
			l.banner(sourceName(t.Name) + " did not complete: " + t.Error)
		}
	}
	if r.Summary.Partial {
		l.banner("This report is partial: not every tool produced a result.")
	}

	l.heading("Executive summary")
	s := r.Summary
	if r.CostPerformance != nil {
		g := "Efficiency grade (Tokvera): " + gradeLabel(s.Grade)
		if s.GradeProvisional {
			g += " · Provisional scoring"
		}
		if s.ReliabilityConcern != "" {
			g += " · reliability concern"
		}
		l.text(g, 10.5, true, pdf.Black, 0)
		if s.USDPer1kRequests != nil {
			l.text("Cost per 1,000 requests: "+usd(*s.USDPer1kRequests)+" (measured, rates as of "+r.CostPerformance.Cost.PricingAsOf+")", 10, false, pdf.Black, 0)
		} else {
			l.text("Cost per 1,000 requests: not computed", 10, false, pdf.Black, 0)
		}
		if s.Spend != nil {
			l.text(fmt.Sprintf("At your volume: %s a month (%s requests/month you gave × measured cost)", usd(s.Spend.MonthlyUSD), commas(itoa64(s.Spend.MonthlyRequests))), 10, false, pdf.Black, 0)
		}
	}
	if r.AgentSecurity != nil {
		l.text(fmt.Sprintf("Agent security (Sievant): %d critical, %d high · deal risk: %d finding(s)", s.SecurityCounts["critical"], s.SecurityCounts["high"], s.DealRisk), 10.5, true, pdf.Black, 0)
	}
	if s.ReliabilityConcern != "" {
		l.banner("Reliability concern: " + s.ReliabilityConcern + ". Shown beside the grade; it is not counted as deal risk.")
	}
	l.subheading(fmt.Sprintf("Top %d findings, ranked by business impact", len(s.Top)))
	if len(s.Top) == 0 {
		l.text("Neither tool reported a finding.", 10, false, colMuted, 0)
	}
	for i, it := range s.Top {
		head := fmt.Sprintf("%d. %s", i+1, it.Title)
		l.text(head, 10.5, true, pdf.Black, 0)
		tags := sourceName(it.Source) + " " + it.Ref + " · " + it.Dimension
		if it.DealRisk {
			tags += " · deal risk"
		}
		if it.Server != "" {
			tags += " · server " + it.Server
		}
		if it.Where != "" {
			tags += " · " + it.Where
		}
		l.text(tags+" · "+it.Why, 9, false, colMuted, 14)
		if it.Fix != "" {
			l.text("Fix: "+it.Fix, 9, false, pdf.Black, 14)
		}
		l.space(3)
	}

	if c := r.CostPerformance; c != nil {
		l.heading("Cost & performance")
		l.text("From Tokvera: "+c.Target+" · model "+c.Model, 9, false, colMuted, 0)
		if c.Simulated {
			l.banner("Simulated: Tokvera measured its built-in demo server; these numbers describe no real endpoint.")
		}
		l.text(fmt.Sprintf("Efficiency grade: %s (%s / 100, scoring %s)", gradeLabel(c.Grade), points(c.Points), c.ScoringVersion), 11, true, pdf.Black, 0)
		if c.Provisional {
			l.text(TokveraProvisionalLabel+". "+c.ProvisionalNote, 9, false, pdf.Black, 0)
		}
		for _, cp := range c.Caps {
			l.text(cp, 9, false, colMuted, 0)
		}
		l.text(fmt.Sprintf("Requests: %d sent, %d succeeded, %d failed, in %s s.", c.Requests, c.Succeeded, c.Failed, float1(float64(c.DurationMS)/1000)), 9.5, false, pdf.Black, 0)
		if len(c.Levels) > 0 {
			l.space(4)
			cols := []float64{0, 70, 135, 190, 260, 330, 400}
			hdr := []string{"Concurrency", "Requests", "Failed", "TTFT p50", "TTFT p95", "TTFT p99", "Duration p95"}
			l.row(cols, hdr, true)
			for _, lv := range c.Levels {
				l.row(cols, []string{strconv.Itoa(lv.Concurrency), strconv.Itoa(lv.Requests), strconv.Itoa(lv.Failed), ms(lv.TTFTP50MS), ms(lv.TTFTP95MS), ms(lv.TTFTP99MS), ms(lv.DurationP95MS)}, false)
			}
		}
		l.subheading("Cost")
		if c.Cost.USDPer1kRequests != nil {
			t := usd(*c.Cost.USDPer1kRequests) + " per 1,000 requests"
			if c.Cost.Estimate {
				t += " (estimate)"
			}
			t += ", rates as of " + c.Cost.PricingAsOf
			if c.Cost.PricingSource != "" {
				t += ", source: " + c.Cost.PricingSource
			}
			l.text(t, 9.5, false, pdf.Black, 0)
		} else {
			l.text("Cost: not computed. "+c.Cost.NotComputed, 9.5, false, pdf.Black, 0)
		}
		if c.Spend != nil {
			l.text(fmt.Sprintf("At the %s requests a month you gave: %s a month (measured cost × your volume). This is spend, not a saving.", commas(itoa64(c.Spend.MonthlyRequests)), usd(c.Spend.MonthlyUSD)), 9.5, false, pdf.Black, 0)
		}
		l.subheading("Findings")
		if len(c.Findings) == 0 {
			l.text("No findings.", 9.5, false, colMuted, 0)
		}
		for _, it := range c.Findings {
			l.findingTokvera(it)
		}
		for _, p := range c.Privacy {
			l.text(p, 8.5, false, colMuted, 0)
		}
	}

	if a := r.AgentSecurity; a != nil {
		l.heading("Agent security")
		l.text("From Sievant: the MCP server configuration of the AI clients on this machine.", 9, false, colMuted, 0)
		for _, f := range a.Files {
			t := fmt.Sprintf("%s · %s · %s · %d server(s), %d not behind Sievant", f.Client, f.Path, f.Scope, f.Servers, f.Unwrapped)
			if f.Error != "" {
				t += " · " + f.Error
			}
			l.text(t, 9, false, pdf.Black, 0)
		}
		if len(a.Files) == 0 {
			l.text("No MCP client configuration found.", 9, false, colMuted, 0)
		}
		l.text(fmt.Sprintf("Findings: %d critical, %d high, %d medium, %d low, %d info.", a.Counts["critical"], a.Counts["high"], a.Counts["medium"], a.Counts["low"], a.Counts["info"]), 9.5, false, pdf.Black, 0)
		l.space(4)
		for _, it := range a.Findings {
			l.findingSievant(it)
		}
		for _, n := range a.Notes {
			l.text(n, 8.5, false, colMuted, 0)
		}
		if g := a.Gateway; g != nil {
			l.subheading("Gateway coverage (sievant report, " + g.Period + ")")
			l.text(fmt.Sprintf("%s · controls enforced %d of %d · %d requests, %d blocked", g.Coverage, g.ControlsEnforced, g.ControlsTotal, g.Requests, g.Blocked), 9.5, false, pdf.Black, 0)
			for _, gp := range g.Gaps {
				l.text(title(gp.Severity)+": "+gp.Title, 9.5, true, pdf.Black, 0)
				if gp.Fix != "" {
					l.text("Fix: "+gp.Fix, 9, false, pdf.Black, 14)
				}
			}
			l.text(g.Disclaimer, 8.5, false, colMuted, 0)
		}
	}

	l.heading("30-day fix plan")
	for _, w := range r.FixPlan {
		l.subheading(w.Title)
		if len(w.Items) == 0 {
			l.text("Nothing this week.", 9, false, colMuted, 0)
		}
		for _, it := range w.Items {
			l.text("• "+it.Title+" ("+sourceName(it.Source)+" "+it.Ref+")", 9.5, false, pdf.Black, 0)
			if it.Fix != "" {
				l.text(it.Fix, 9, false, colMuted, 12)
			}
		}
	}

	l.heading("How to read this report")
	l.text(r.Ranking.Note, 9, false, pdf.Black, 0)
	l.space(4)
	l.text(r.Privacy, 9, false, pdf.Black, 0)
	if r.CTA != nil {
		l.space(14)
		l.ensure(30)
		label := "Want a second pair of eyes? Book the " + r.CTA.Text + ": " + r.CTA.URL
		l.text(label, 10.5, true, colAccent, 0)
		l.page.Link(marginX, l.y-2, pdf.Width(label, 10.5, true), 14, r.CTA.URL)
	}
	l.space(10)
	l.text("Generated by "+r.Generator+" · schema "+r.SchemaVersion, 8, false, colMuted, 0)
	_, err := l.doc.WriteTo(w)
	return err
}

func (l *layout) row(cols []float64, cells []string, bold bool) {
	l.ensure(14)
	l.y -= 13
	for i, c := range cells {
		l.page.Text(marginX+cols[i], l.y+3, 8.5, bold, pdf.Black, c)
	}
	l.page.Line(marginX, l.y, marginX+contentW, l.y, 0.4, colLine)
}

func (l *layout) findingTokvera(it scoringItem) {
	head := it.Title + " · " + it.Status + " · −" + strconv.FormatFloat(it.Impact, 'f', -1, 64) + " grade points"
	if it.Effort != "" {
		head += " · effort " + it.Effort
	}
	l.text(head, 9.5, true, pdf.Black, 0)
	for _, s := range []string{it.Evidence, it.WhyItMatters} {
		if s != "" {
			l.text(s, 9, false, colMuted, 12)
		}
	}
	if it.Fix != "" {
		l.text("Fix: "+it.Fix, 9, false, pdf.Black, 12)
	}
	l.space(3)
}

func (l *layout) findingSievant(it scoringItem) {
	head := title(it.Severity) + ": " + it.Title + " (" + it.Ref + ")"
	if it.DealRisk {
		head += " · deal risk"
	}
	l.text(head, 9.5, true, pdf.Black, 0)
	loc := it.Client + " · " + it.File
	if it.Server != "" {
		loc += " · server " + it.Server
	}
	if it.Where != "" {
		loc += " · " + it.Where
	}
	l.text(loc, 8.5, false, colMuted, 12)
	if it.Evidence != "" {
		l.text("Evidence (masked): "+it.Evidence, 9, false, pdf.Black, 12)
	}
	if it.Fix != "" {
		l.text("Fix: "+it.Fix, 9, false, pdf.Black, 12)
	}
	l.space(3)
}
