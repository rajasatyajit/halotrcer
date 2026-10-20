// Package scoring ranks the findings of both tools by business impact
// (docs/scoring.md). It only orders what the tools measured: it computes no
// new measurement, invents no price and claims no savings (PLAN.md D4).
package scoring

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rajasatyajit/halotrcer/internal/ingest"
)

// Version is the scoring model's version. While it is 0.x the model is
// provisional, like Tokvera's: its tiers are judgement, not calibrated.
const Version = "0.1.0"

// Dimensions an item is filed under.
const (
	DimCost        = "cost"
	DimLatency     = "latency"
	DimOperational = "operational"
	DimSecurity    = "security"
)

// DealRiskRules are the Sievant rules a customer security review would flag
// (PLAN.md D3): credentials on disk (SEC001-SEC004), an unauthenticated
// remote server (AUTH001), plaintext HTTP (NET001), and the filesystem root
// or home directory exposed (PERM001, PERM002). Deal risk comes from these
// rules only.
var DealRiskRules = map[string]bool{
	"SEC001": true, "SEC002": true, "SEC003": true, "SEC004": true,
	"AUTH001": true, "NET001": true, "PERM001": true, "PERM002": true,
}

var severityWeight = map[string]float64{
	"critical": 1.0, "high": 0.75, "medium": 0.5, "low": 0.25, "info": 0,
}

// Item is one finding, from either tool, placed in the ranking.
type Item struct {
	Source    string  `json:"source"` // "tokvera" or "sievant"
	Ref       string  `json:"ref"`    // Tokvera check_id or Sievant rule
	Title     string  `json:"title"`
	Dimension string  `json:"dimension"`
	Tier      int     `json:"tier"`
	Weight    float64 `json:"weight"`
	DealRisk  bool    `json:"deal_risk"`
	Why       string  `json:"why"`
	Week      int     `json:"week"` // in the 30-day fix plan, 1-4

	// Source fields, carried unchanged.
	Status       string  `json:"status,omitempty"`   // Tokvera warn|fail
	Impact       float64 `json:"impact,omitempty"`   // Tokvera grade points lost
	Rank         int     `json:"rank,omitempty"`     // Tokvera rank
	Effort       string  `json:"effort,omitempty"`   // Tokvera effort
	Preview      bool    `json:"preview,omitempty"`  // Tokvera preview check
	Severity     string  `json:"severity,omitempty"` // Sievant severity
	Evidence     string  `json:"evidence,omitempty"`
	WhyItMatters string  `json:"why_it_matters,omitempty"`
	Fix          string  `json:"fix,omitempty"`
	Detail       string  `json:"detail,omitempty"`
	Client       string  `json:"client,omitempty"`
	File         string  `json:"file,omitempty"`
	Server       string  `json:"server,omitempty"`
	Where        string  `json:"where,omitempty"`
}

// Result is the ranked list and the report-level signals.
type Result struct {
	ScoringVersion string `json:"scoring_version"`
	Provisional    bool   `json:"provisional"`
	Items          []Item `json:"items"`
	// ReliabilityConcern is set when Tokvera graded the endpoint D or F
	// (PLAN.md D3): shown beside the grade, never counted as deal risk.
	ReliabilityConcern string `json:"reliability_concern,omitempty"`
}

// Top returns the first n items (the executive summary uses 5).
func (r Result) Top(n int) []Item {
	if len(r.Items) < n {
		n = len(r.Items)
	}
	return r.Items[:n]
}

// Week returns the fix plan's items for week w (1-4), in ranking order.
func (r Result) Week(w int) []Item {
	var out []Item
	for _, it := range r.Items {
		if it.Week == w {
			out = append(out, it)
		}
	}
	return out
}

// Score ranks both tools' findings. Either report may be nil.
func Score(tok *ingest.TokveraReport, sv *ingest.SievantScan) Result {
	res := Result{ScoringVersion: Version, Provisional: true, Items: []Item{}}
	if tok != nil {
		res.Items = append(res.Items, tokveraItems(tok)...)
		if g := tok.Score.Grade; g == "D" || g == "F" {
			res.ReliabilityConcern = fmt.Sprintf("Tokvera graded this endpoint %s", g)
		}
	}
	if sv != nil {
		res.Items = append(res.Items, sievantItems(sv)...)
	}
	sort.SliceStable(res.Items, func(i, j int) bool { return less(res.Items[i], res.Items[j]) })
	for i := range res.Items {
		res.Items[i].Week = week(res.Items[i])
	}
	return res
}

func pillarDimension(p string) string {
	switch p {
	case "cost_and_cache":
		return DimCost
	case "latency", "capacity", "reliability":
		return DimLatency
	default:
		return DimOperational
	}
}

func tokveraItems(r *ingest.TokveraReport) []Item {
	pillar := map[string]string{}
	for _, c := range r.Checks {
		pillar[c.ID] = c.Pillar
	}
	var maxImpact float64
	var impacts []float64
	for _, f := range r.Findings {
		maxImpact = math.Max(maxImpact, f.Impact)
		impacts = append(impacts, f.Impact)
	}
	med := median(impacts)
	var out []Item
	for _, f := range r.Findings {
		it := Item{
			Source: "tokvera", Ref: f.CheckID, Title: f.Title,
			Dimension: pillarDimension(pillar[f.CheckID]),
			Status:    f.Status, Impact: f.Impact, Rank: f.Rank, Effort: f.Effort, Preview: f.Preview,
			Evidence: f.Evidence, WhyItMatters: f.WhyItMatters, Fix: f.SuggestedFix,
		}
		if maxImpact > 0 {
			it.Weight = f.Impact / maxImpact
		}
		if f.Status != "fail" {
			it.Weight *= 0.5
		}
		costOrLatency := it.Dimension == DimCost || it.Dimension == DimLatency
		switch {
		case f.Preview:
			it.Tier = 5
			it.Why = "preview check: Tokvera does not count it in the grade"
		case f.Status == "fail" && costOrLatency && f.Impact >= med:
			it.Tier = 2
			it.Why = fmt.Sprintf("Tokvera fail on %s, impact %s grade points (at or above the median %s)", it.Dimension, num(f.Impact), num(med))
		case f.Status == "fail":
			it.Tier = 4
			it.Why = fmt.Sprintf("Tokvera fail on %s, impact %s grade points", it.Dimension, num(f.Impact))
		default:
			it.Tier = 5
			it.Why = fmt.Sprintf("Tokvera warning on %s, impact %s grade points", it.Dimension, num(f.Impact))
		}
		out = append(out, it)
	}
	return out
}

func sievantItems(r *ingest.SievantScan) []Item {
	var out []Item
	for _, f := range r.Findings {
		it := Item{
			Source: "sievant", Ref: f.Rule, Title: f.Title, Dimension: DimSecurity,
			Severity: f.Severity, Weight: severityWeight[f.Severity],
			DealRisk: DealRiskRules[f.Rule],
			Evidence: f.Evidence, Fix: f.Fix, Detail: f.Detail,
			Client: f.Client, File: f.File, Server: f.Server, Where: f.Where,
		}
		highOrAbove := f.Severity == "critical" || f.Severity == "high"
		switch {
		case f.Severity == "critical":
			it.Tier = 1
			it.Why = fmt.Sprintf("critical security finding (Sievant %s)", f.Rule)
		case it.DealRisk && highOrAbove:
			it.Tier = 1
			it.Why = fmt.Sprintf("deal risk: a customer security review would flag Sievant %s (%s)", f.Rule, f.Severity)
		case f.Severity == "high":
			it.Tier = 3
			it.Why = fmt.Sprintf("high security finding (Sievant %s)", f.Rule)
		default:
			it.Tier = 5
			it.Why = fmt.Sprintf("%s security finding (Sievant %s)", f.Severity, f.Rule)
		}
		if it.DealRisk && it.Tier != 1 {
			it.Why += "; deal risk"
		}
		out = append(out, it)
	}
	return out
}

// less orders by tier, then weight, then a deterministic tie-break: Tokvera
// items by rank before Sievant items by rule and location.
func less(a, b Item) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	if a.Weight != b.Weight {
		return a.Weight > b.Weight
	}
	if a.Source != b.Source {
		return a.Source == "tokvera"
	}
	if a.Source == "tokvera" {
		return a.Rank < b.Rank
	}
	for _, p := range [][2]string{{a.Ref, b.Ref}, {a.File, b.File}, {a.Server, b.Server}, {a.Where, b.Where}, {a.Title, b.Title}} {
		if p[0] != p[1] {
			return p[0] < p[1]
		}
	}
	return false
}

// week places an item in the 30-day fix plan: week 1 for tier 1 and for
// any Tokvera fix estimated under a day; weeks 2 and 3 for tiers 2 and 3;
// week 4 for the rest.
func week(it Item) int {
	if it.Tier == 1 {
		return 1
	}
	if it.Source == "tokvera" && !it.Preview && underADay(it.Effort) {
		return 1
	}
	switch it.Tier {
	case 2:
		return 2
	case 3:
		return 3
	default:
		return 4
	}
}

var numRE = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// underADay reads Tokvera's effort strings ("0.5 days", "1–3 days",
// "2–5 days"): the upper bound must be under one day. An effort in hours
// counts as under a day when its upper bound is under 8 hours. Anything else
// is not.
func underADay(effort string) bool {
	e := strings.ToLower(effort)
	nums := numRE.FindAllString(e, -1)
	if len(nums) == 0 {
		return false
	}
	hi, err := strconv.ParseFloat(nums[len(nums)-1], 64)
	if err != nil {
		return false
	}
	switch {
	case strings.Contains(e, "hour"):
		return hi < 8
	case strings.Contains(e, "day"):
		return hi < 1
	default:
		return false
	}
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func num(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Spend is the only dollar figure Halotrcer shows (PLAN.md D4): Tokvera's
// measured cost per 1,000 requests times the monthly request volume the user
// supplies, labelled with the rate sheet's date and source.
type Spend struct {
	USDPer1kRequests float64 `json:"usd_per_1k_requests"`
	MonthlyRequests  int64   `json:"monthly_requests"`
	MonthlyUSD       float64 `json:"monthly_usd"`
	PricingAsOf      string  `json:"pricing_as_of"`
	PricingSource    string  `json:"pricing_source"`
	Estimate         bool    `json:"estimate"`
}

// MonthlySpend returns nil unless Tokvera computed a cost and the user gave
// a positive monthly volume.
func MonthlySpend(r *ingest.TokveraReport, monthlyRequests int64) *Spend {
	if r == nil || r.Cost.USDPer1kRequests == nil || monthlyRequests <= 0 {
		return nil
	}
	per1k := *r.Cost.USDPer1kRequests
	return &Spend{
		USDPer1kRequests: per1k,
		MonthlyRequests:  monthlyRequests,
		MonthlyUSD:       per1k * float64(monthlyRequests) / 1000,
		PricingAsOf:      r.Cost.PricingAsOf,
		PricingSource:    r.Cost.PricingSource,
		Estimate:         r.Cost.Estimate,
	}
}
