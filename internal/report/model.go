// Package report builds Halotrcer's combined report from both tools'
// results and renders it as JSON, self-contained HTML and PDF.
//
// Every string in the model passes through internal/redact when the model
// is built, so the three formats can only show what the redactor let
// through.
package report

import (
	"time"

	"github.com/rajasatyajit/halotrcer/internal/scoring"
)

// SchemaVersion of the combined JSON. Fields are only ever added within v1.
const SchemaVersion = "halotrcer.check-report/v1.0.0"

// The CTA (removable with --no-cta).
const (
	CTAText = "AI Cost & Security Audit"
	CTAURL  = "https://www.halotrcer.com/book"
)

// Tokvera's provisional label and note, as Tokvera documents them
// (tokvera docs/scoring.md:94, also score.ProvisionalLabel and
// score.ProvisionalNote). Shown, unchanged, whenever its report has
// score.provisional true; report.json carries the flag, not the words.
const (
	TokveraProvisionalLabel = "Provisional scoring"
	TokveraProvisionalNote  = "The weights and thresholds are provisional: they have not yet been calibrated against real pilot scans and may change before scoring 1.0.0."
)

// RankingNote explains Halotrcer's own ranking, which is provisional too.
const RankingNote = "Halotrcer's ranking across the two tools is provisional (scoring version " + scoring.Version +
	"): its tiers are judgement, not calibrated. See docs/scoring.md."

// PrivacyStatement is Halotrcer's.
const PrivacyStatement = "Halotrcer contacts only the target you give it (through Tokvera) and reads only the MCP " +
	"configuration files Sievant reads, on this machine. It sends no telemetry. Secrets stay masked as each tool " +
	"masks them; on top of that, Halotrcer drops URL credentials and query strings, hides environment values, " +
	"shows your home directory as ~ and replaces your user name with <user>."

// Report is the combined report.
type Report struct {
	SchemaVersion string    `json:"schema_version"`
	Generator     string    `json:"generator"`
	GeneratedAt   time.Time `json:"generated_at"`
	Target        string    `json:"target,omitempty"`
	Tools         []ToolRun `json:"tools"`

	Summary         Summary          `json:"summary"`
	CostPerformance *CostPerformance `json:"cost_performance,omitempty"`
	AgentSecurity   *AgentSecurity   `json:"agent_security,omitempty"`
	FixPlan         []PlanWeek       `json:"fix_plan"`
	Ranking         Ranking          `json:"ranking"`
	Privacy         string           `json:"privacy"`
	CTA             *CTA             `json:"cta,omitempty"`
}

// ToolRun records one tool's part in the run.
type ToolRun struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status"` // ok, failed, skipped
	Error   string `json:"error,omitempty"`
	// CompatWarning is set when the version is outside the matrix.
	CompatWarning string `json:"compat_warning,omitempty"`
}

// Summary is the one-screen executive summary.
type Summary struct {
	Top                []scoring.Item `json:"top"`
	Grade              string         `json:"grade,omitempty"`
	GradeProvisional   bool           `json:"grade_provisional,omitempty"`
	USDPer1kRequests   *float64       `json:"usd_per_1k_requests,omitempty"`
	Spend              *scoring.Spend `json:"spend,omitempty"`
	SecurityCounts     map[string]int `json:"security_counts,omitempty"`
	DealRisk           int            `json:"deal_risk"`
	ReliabilityConcern string         `json:"reliability_concern,omitempty"`
	Partial            bool           `json:"partial,omitempty"`
}

// CostPerformance is the Tokvera section.
type CostPerformance struct {
	Target          string         `json:"target"`
	Model           string         `json:"model"`
	Simulated       bool           `json:"simulated"`
	SimulatedTiming bool           `json:"simulated_timing"`
	Grade           string         `json:"grade"`
	Points          *float64       `json:"points,omitempty"`
	GradeReason     string         `json:"grade_reason,omitempty"`
	ScoringVersion  string         `json:"scoring_version"`
	Provisional     bool           `json:"provisional"`
	ProvisionalNote string         `json:"provisional_note,omitempty"`
	Caps            []string       `json:"caps,omitempty"`
	Requests        int            `json:"requests"`
	Succeeded       int            `json:"succeeded"`
	Failed          int            `json:"failed"`
	DurationMS      int64          `json:"duration_ms"`
	Levels          []Level        `json:"levels"`
	Cost            Cost           `json:"cost"`
	Spend           *scoring.Spend `json:"spend,omitempty"`
	Findings        []scoring.Item `json:"findings"`
	Privacy         []string       `json:"privacy"`
}

// Level is one concurrency level's latency, from Tokvera's levels[].
type Level struct {
	Concurrency   int     `json:"concurrency"`
	Requests      int     `json:"requests"`
	Failed        int     `json:"failed"`
	TTFTP50MS     float64 `json:"ttft_p50_ms"`
	TTFTP95MS     float64 `json:"ttft_p95_ms"`
	TTFTP99MS     float64 `json:"ttft_p99_ms"`
	DurationP95MS float64 `json:"request_duration_p95_ms"`
	TokensPerSP50 float64 `json:"output_tokens_per_s_p50"`
}

// Cost is Tokvera's cost figure and the rate sheet behind it.
type Cost struct {
	USDPer1kRequests *float64 `json:"usd_per_1k_requests,omitempty"`
	NotComputed      string   `json:"not_computed,omitempty"`
	Estimate         bool     `json:"estimate"`
	Basis            string   `json:"basis,omitempty"`
	PricingSheet     string   `json:"pricing_sheet,omitempty"`
	PricingAsOf      string   `json:"pricing_as_of,omitempty"`
	PricingSource    string   `json:"pricing_source,omitempty"`
	PricingStale     bool     `json:"pricing_stale,omitempty"`
	PricingWarning   string   `json:"pricing_warning,omitempty"`
}

// AgentSecurity is the Sievant section.
type AgentSecurity struct {
	Files          []ConfigFile   `json:"files"`
	Counts         map[string]int `json:"counts"`
	ToolsInspected bool           `json:"tools_inspected"`
	Findings       []scoring.Item `json:"findings"`
	Notes          []string       `json:"notes,omitempty"`
	Gateway        *Gateway       `json:"gateway,omitempty"`
}

// ConfigFile is one MCP client configuration Sievant read.
type ConfigFile struct {
	Client    string `json:"client"`
	Path      string `json:"path"`
	Scope     string `json:"scope"`
	Servers   int    `json:"servers"`
	Unwrapped int    `json:"unwrapped"`
	Error     string `json:"error,omitempty"`
}

// Gateway is the optional `sievant report` summary (--gateway-report).
type Gateway struct {
	Period           string   `json:"period"`
	Coverage         string   `json:"coverage"`
	ControlsEnforced int      `json:"controls_enforced"`
	ControlsTotal    int      `json:"controls_total"`
	Requests         int      `json:"requests"`
	Blocked          int      `json:"blocked"`
	Gaps             []GapRow `json:"gaps"`
	Disclaimer       string   `json:"disclaimer"`
}

// GapRow is one Sievant report gap.
type GapRow struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Fix      string `json:"fix"`
}

// PlanWeek is one week of the 30-day fix plan.
type PlanWeek struct {
	Week  int        `json:"week"`
	Title string     `json:"title"`
	Items []PlanItem `json:"items"`
}

// PlanItem is one fix: the tool's own fix text.
type PlanItem struct {
	Source string `json:"source"`
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Fix    string `json:"fix"`
	Effort string `json:"effort,omitempty"`
	Where  string `json:"where,omitempty"`
}

// Ranking describes Halotrcer's ranking model.
type Ranking struct {
	ScoringVersion string `json:"scoring_version"`
	Provisional    bool   `json:"provisional"`
	Note           string `json:"note"`
}

// CTA is the footer call to action.
type CTA struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}
