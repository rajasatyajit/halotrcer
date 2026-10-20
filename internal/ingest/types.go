package ingest

import "time"

// TokveraReport is the subset of Tokvera's report.json that Halotrcer reads.
type TokveraReport struct {
	SchemaVersion   string           `json:"schema_version"`
	ScoringVersion  string           `json:"scoring_version"`
	Tool            Tool             `json:"tool"`
	GeneratedAt     time.Time        `json:"generated_at"`
	Simulated       bool             `json:"simulated"`
	SimulatedTiming bool             `json:"simulated_timing"`
	Target          TokveraTarget    `json:"target"`
	Model           string           `json:"model"`
	Run             TokveraRun       `json:"run"`
	Levels          []TokveraLevel   `json:"levels"`
	Cost            TokveraCost      `json:"cost"`
	Pricing         TokveraPricing   `json:"pricing"`
	Checks          []TokveraCheck   `json:"checks"`
	Findings        []TokveraFinding `json:"findings"`
	Score           TokveraScore     `json:"score"`
	Privacy         TokveraPrivacy   `json:"privacy"`
}

// Tool names the tool and version that wrote a report.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TokveraTarget is the scanned endpoint; url has userinfo and query stripped
// by Tokvera (tokvera pkg/scan/target.go).
type TokveraTarget struct {
	URL      string `json:"url"`
	Origin   string `json:"origin"`
	BaseURL  string `json:"base_url"`
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	Loopback bool   `json:"loopback"`
}

// TokveraRun summarizes the load.
type TokveraRun struct {
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int64     `json:"duration_ms"`
	Requests      int       `json:"requests"`
	Succeeded     int       `json:"succeeded"`
	Failed        int       `json:"failed"`
	BudgetStopped bool      `json:"budget_stopped"`
	Cancelled     bool      `json:"cancelled"`
}

// TokveraLevel is one concurrency level's measurements.
type TokveraLevel struct {
	Concurrency       int   `json:"concurrency"`
	Requests          int   `json:"requests"`
	OK                int   `json:"ok"`
	Failed            int   `json:"failed"`
	Timeouts          int   `json:"timeouts"`
	TTFTMS            Stats `json:"ttft_ms"`
	RequestDurationMS Stats `json:"request_duration_ms"`
	OutputTokensPerS  Stats `json:"output_tokens_per_s"`
}

// Stats is Tokvera's exact nearest-rank summary (tokvera pkg/bench/results.go).
type Stats struct {
	Count int     `json:"count"`
	Min   float64 `json:"min"`
	Mean  float64 `json:"mean"`
	P50   float64 `json:"p50"`
	P90   float64 `json:"p90"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
	Max   float64 `json:"max"`
}

// TokveraCost is the measured cost. USDPer1kRequests is nil when Tokvera did
// not compute it ("not computed", with the reason in Note).
type TokveraCost struct {
	Provider          string   `json:"provider"`
	USDPer1kRequests  *float64 `json:"usd_per_1k_requests"`
	RequestsPriced    int      `json:"requests_priced"`
	RequestsSucceeded int      `json:"requests_succeeded"`
	PricingAsOf       string   `json:"pricing_as_of"`
	PricingSource     string   `json:"pricing_source"`
	Estimate          bool     `json:"estimate"`
	Basis             string   `json:"basis"`
	Note              string   `json:"note"`
}

// TokveraPricing describes the rate sheet used.
type TokveraPricing struct {
	Sheet    string `json:"sheet"`
	Embedded bool   `json:"embedded"`
	AsOf     string `json:"as_of"`
	Source   string `json:"source"`
	AgeDays  *int   `json:"age_days"`
	Stale    bool   `json:"stale"`
	Warning  string `json:"warning"`
	Estimate bool   `json:"estimate"`
}

// TokveraCheck is one graded check; Pillar is latency, reliability,
// capacity, observability or cost_and_cache (tokvera docs/scoring.md).
type TokveraCheck struct {
	ID        string   `json:"id"`
	Pillar    string   `json:"pillar"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Preview   bool     `json:"preview"`
	Value     *float64 `json:"value"`
	Unit      string   `json:"unit"`
	PassBound float64  `json:"pass_bound"`
	WarnBound float64  `json:"warn_bound"`
	Evidence  string   `json:"evidence"`
}

// TokveraFinding is a failed or warned check, ranked by Impact (grade points
// lost).
type TokveraFinding struct {
	Rank         int     `json:"rank"`
	CheckID      string  `json:"check_id"`
	Status       string  `json:"status"`
	Preview      bool    `json:"preview"`
	Title        string  `json:"title"`
	Evidence     string  `json:"evidence"`
	WhyItMatters string  `json:"why_it_matters"`
	SuggestedFix string  `json:"suggested_fix"`
	Effort       string  `json:"effort"`
	Impact       float64 `json:"impact"`
}

// TokveraScore is the Efficiency Score. Grade is A-F or
// "insufficient_evidence"; Points is nil without a grade.
type TokveraScore struct {
	ScoringVersion string       `json:"scoring_version"`
	Provisional    bool         `json:"provisional"`
	Grade          string       `json:"grade"`
	Reason         string       `json:"reason"`
	Points         *float64     `json:"points"`
	UncappedGrade  string       `json:"uncapped_grade"`
	CapsApplied    []TokveraCap `json:"caps_applied"`
}

// TokveraCap is a cap that bound the grade.
type TokveraCap struct {
	ID       string `json:"id"`
	MaxGrade string `json:"max_grade"`
	Reason   string `json:"reason"`
}

// TokveraPrivacy is Tokvera's fixed privacy text.
type TokveraPrivacy struct {
	Statement string `json:"statement"`
	Prompts   string `json:"prompts"`
}

// SievantScan is `sievant scan --json`.
type SievantScan struct {
	ReportVersion  int              `json:"report_version"`
	Generator      string           `json:"generator"`
	GeneratedAt    string           `json:"generated_at"`
	ToolsInspected bool             `json:"tools_inspected"`
	Files          []SievantFile    `json:"files"`
	Findings       []SievantFinding `json:"findings"`
	Counts         map[string]int   `json:"counts"`
	Notes          []string         `json:"notes"`
}

// SievantFile is one client configuration file. Path is as given or
// discovered: absolute and not shortened (Halotrcer redacts it).
type SievantFile struct {
	Client  string          `json:"client"`
	Path    string          `json:"path"`
	Scope   string          `json:"scope"`
	Error   string          `json:"error"`
	Servers []SievantServer `json:"servers"`
}

// SievantServer is one MCP server entry.
type SievantServer struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Wrapped   bool   `json:"wrapped"`
	Disabled  bool   `json:"disabled"`
	Findings  int    `json:"findings"`
}

// SievantFinding is one scan finding; Evidence is already masked by Sievant.
type SievantFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Client   string `json:"client"`
	File     string `json:"file"`
	Server   string `json:"server"`
	Where    string `json:"where"`
	Evidence string `json:"evidence"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
}

// SievantGateway is the subset of `sievant report --format json` shown
// behind --gateway-report.
type SievantGateway struct {
	ReportVersion int              `json:"report_version"`
	Generator     string           `json:"generator"`
	Period        SievantPeriod    `json:"period"`
	Summary       SievantSummary   `json:"summary"`
	Controls      []SievantControl `json:"controls"`
	Gaps          []SievantGap     `json:"gaps"`
	Disclaimer    string           `json:"disclaimer"`
}

// SievantPeriod is the report window.
type SievantPeriod struct {
	Label string `json:"label"`
}

// SievantSummary carries the coverage and counts.
type SievantSummary struct {
	Coverage         SievantCoverage `json:"coverage"`
	Requests         int             `json:"requests"`
	Blocked          int             `json:"blocked"`
	ControlsEnforced int             `json:"controls_enforced"`
	ControlsTotal    int             `json:"controls_total"`
}

// SievantCoverage says how much MCP traffic passes through Sievant.
type SievantCoverage struct {
	State          string `json:"state"`
	ThroughSievant int    `json:"through_sievant"`
	Servers        int    `json:"servers"`
	Headline       string `json:"headline"`
}

// SievantControl is one control's status.
type SievantControl struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// SievantGap is a coverage or configuration gap, with its fix.
type SievantGap struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
}

// Doctor is either tool's `doctor --json`.
type Doctor struct {
	Checks   []DoctorCheck `json:"checks"`
	Warnings int           `json:"warnings"`
	Failures int           `json:"failures"`
}

// DoctorCheck is one doctor check; Status is ok, info, warn or fail.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}
