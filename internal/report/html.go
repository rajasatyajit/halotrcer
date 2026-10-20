package report

import (
	"html/template"
	"io"
)

var funcs = template.FuncMap{
	"usd": usd, "ms": ms, "float1": float1, "points": points, "grade": gradeLabel,
	"title": title, "source": sourceName, "impact": impactLabel, "commas": func(n int64) string { return commas(itoa64(n)) },
	"add":   func(a, b int) int { return a + b },
	"divms": func(n int64) float64 { return float64(n) / 1000 },
	"deref": func(p *float64) float64 { return *p },
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var page = template.Must(template.New("report").Funcs(funcs).Parse(htmlTemplate))

// WriteHTML writes the report as one self-contained page: inline CSS, no
// scripts, no external resources, and a Content-Security-Policy that
// forbids loading any.
func (r *Report) WriteHTML(w io.Writer) error {
	return page.Execute(w, r)
}

const htmlTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<meta name="referrer" content="no-referrer">
<title>Halotrcer report{{with .Target}}: {{.}}{{end}}</title>
<style>
:root{--bg:#fbfbf9;--fg:#1d2126;--muted:#5d6670;--line:#dfe2e5;--card:#fff;--accent:#1f5f8b;--crit:#a3262a;--high:#c2561a;--med:#9a6f00;--low:#4b6b8a;--ok:#2c7a4b;--tag:#eef1f4}
@media (prefers-color-scheme:dark){:root{--bg:#14171a;--fg:#e4e7ea;--muted:#9aa3ad;--line:#2c3238;--card:#1b1f23;--accent:#6fb0de;--crit:#ef6b6f;--high:#f0955c;--med:#e3be4f;--low:#8fb0cf;--ok:#6cc391;--tag:#262c32}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
main{max-width:1040px;margin:0 auto;padding:24px 16px 48px}
h1{font-size:26px;margin:0 0 4px}h2{font-size:20px;margin:36px 0 12px;padding-bottom:6px;border-bottom:1px solid var(--line)}h3{font-size:16px;margin:20px 0 8px}
.muted{color:var(--muted)}.small{font-size:13px}
.banner{border:1px solid var(--line);border-left:4px solid var(--high);background:var(--card);padding:10px 14px;margin:12px 0;border-radius:6px}
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px;margin:12px 0 18px}
.tile{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:12px 14px}
.tile .k{font-size:12px;text-transform:uppercase;letter-spacing:.04em;color:var(--muted)}.tile .v{font-size:24px;font-weight:650;margin-top:2px}.tile .n{font-size:13px;color:var(--muted)}
.tag{display:inline-block;font-size:12px;padding:1px 7px;border-radius:10px;background:var(--tag);color:var(--muted);margin-left:4px;white-space:nowrap}
.tag.deal{color:var(--crit);font-weight:600}
.sev{font-weight:600;text-transform:capitalize}.sev-critical{color:var(--crit)}.sev-high{color:var(--high)}.sev-medium{color:var(--med)}.sev-low,.sev-info{color:var(--low)}.st-fail{color:var(--crit);font-weight:600}.st-warn{color:var(--med);font-weight:600}
.scroll{overflow-x:auto}
table{border-collapse:collapse;width:100%;font-size:14px;background:var(--card)}th,td{text-align:left;vertical-align:top;padding:7px 9px;border-bottom:1px solid var(--line)}th{font-size:12px;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);font-weight:600}
td.num,th.num{text-align:right;font-variant-numeric:tabular-nums}
ol.top{padding-left:22px;margin:0}ol.top li{margin:0 0 12px}
.find{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:10px 14px;margin:0 0 10px}.find p{margin:4px 0}
code{font:13px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:var(--tag);padding:1px 4px;border-radius:4px;overflow-wrap:anywhere}
.week{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:10px 14px;margin:0 0 12px}.week ul{margin:6px 0 0;padding-left:20px}.week li{margin:0 0 6px}
footer{margin-top:40px;padding-top:16px;border-top:1px solid var(--line)}
.cta{display:block;background:var(--card);border:1px solid var(--line);border-left:4px solid var(--accent);border-radius:8px;padding:14px 16px;margin:16px 0}.cta a{color:var(--accent);font-weight:650}
a{color:var(--accent)}
@media print{body{background:#fff;color:#000}main{max-width:none;padding:0}.tile,.find,.week,.cta,table{break-inside:avoid}a{color:#000}}
</style>
</head>
<body>
<main>
<header>
<h1>Halotrcer report</h1>
<div class="muted">{{with .Target}}Target <code>{{.}}</code> · {{end}}{{.GeneratedAt.Format "2006-01-02 15:04 UTC"}} · {{.Generator}}</div>
<div class="muted small">{{range $i, $t := .Tools}}{{if $i}} · {{end}}{{source $t.Name}} {{if $t.Version}}{{$t.Version}} {{end}}({{$t.Status}}){{end}}</div>
{{range .Tools}}{{if .CompatWarning}}<div class="banner small">{{.CompatWarning}}</div>{{end}}{{if .Error}}<div class="banner"><strong>{{source .Name}} did not complete:</strong> {{.Error}}</div>{{end}}{{end}}
{{if .Summary.Partial}}<div class="banner">This report is partial: not every tool produced a result.</div>{{end}}
</header>

<section id="summary">
<h2>Executive summary</h2>
<div class="tiles">
{{if .CostPerformance}}<div class="tile"><div class="k">Efficiency grade (Tokvera)</div><div class="v">{{grade .Summary.Grade}}</div><div class="n">{{if .Summary.GradeProvisional}}{{.CostPerformance.ProvisionalLabel}}{{end}}{{with .Summary.ReliabilityConcern}} · reliability concern{{end}}</div></div>
<div class="tile"><div class="k">Cost per 1,000 requests</div>{{with .Summary.USDPer1kRequests}}<div class="v">{{usd .}}</div><div class="n">measured, {{if $.CostPerformance.Cost.Estimate}}estimate {{end}}at {{$.CostPerformance.Cost.PricingAsOf}} rates</div>{{else}}<div class="v">—</div><div class="n">not computed</div>{{end}}</div>
{{with .Summary.Spend}}<div class="tile"><div class="k">At your volume</div><div class="v">{{usd .MonthlyUSD}}/mo</div><div class="n">{{commas .MonthlyRequests}} requests/month you gave × measured cost</div></div>{{end}}{{end}}
{{if .AgentSecurity}}<div class="tile"><div class="k">Agent security (Sievant)</div><div class="v">{{index .Summary.SecurityCounts "critical"}} critical · {{index .Summary.SecurityCounts "high"}} high</div><div class="n">{{len .AgentSecurity.Files}} MCP configuration file(s) read</div></div>
<div class="tile"><div class="k">Deal risk</div><div class="v">{{.Summary.DealRisk}}</div><div class="n">findings a customer security review would flag</div></div>{{end}}
</div>
{{with .Summary.ReliabilityConcern}}<p class="banner"><strong>Reliability concern:</strong> {{.}}. Shown beside the grade; it is not counted as deal risk.</p>{{end}}
<h3>Top {{len .Summary.Top}} findings, ranked by business impact</h3>
{{if .Summary.Top}}<ol class="top">{{range .Summary.Top}}
<li><strong>{{.Title}}</strong>{{if .DealRisk}}<span class="tag deal">deal risk</span>{{end}}<span class="tag">{{source .Source}} {{.Ref}}</span><span class="tag">{{.Dimension}}</span><br>
<span class="small muted">{{.Why}}{{with .Server}} · server <code>{{.}}</code>{{end}}{{with .Where}} · <code>{{.}}</code>{{end}}</span>{{with .Fix}}<br><span class="small">Fix: {{.}}</span>{{end}}</li>{{end}}
</ol>{{else}}<p class="muted">Neither tool reported a finding.</p>{{end}}
</section>

{{with .CostPerformance}}<section id="cost-performance">
<h2>Cost &amp; performance</h2>
<p class="small muted">From Tokvera: <code>{{.Target}}</code>{{with .Model}}, model <code>{{.}}</code>{{end}}.</p>
{{if .Simulated}}<div class="banner"><strong>Simulated.</strong> Tokvera measured its built-in demo server{{if .SimulatedTiming}} with declared, not measured, timings{{end}}; these numbers describe no real endpoint.</div>{{end}}
<p><strong>Efficiency grade: {{grade .Grade}}</strong> ({{points .Points}} / 100, scoring {{.ScoringVersion}}){{with .GradeReason}} · {{.}}{{end}}</p>
{{if .Provisional}}<p class="small"><strong>{{.ProvisionalLabel}}.</strong> {{.ProvisionalNote}}</p>{{end}}
{{range .Caps}}<p class="small muted">{{.}}</p>{{end}}
<p class="small">Requests: {{.Requests}} sent, {{.Succeeded}} succeeded, {{.Failed}} failed, in {{float1 (divms .DurationMS)}} s.</p>
{{if .Levels}}<div class="scroll"><table>
<tr><th class="num">Concurrency</th><th class="num">Requests</th><th class="num">Failed</th><th class="num">TTFT p50</th><th class="num">TTFT p95</th><th class="num">TTFT p99</th><th class="num">Duration p95</th><th class="num">Tokens/s p50</th></tr>
{{range .Levels}}<tr><td class="num">{{.Concurrency}}</td><td class="num">{{.Requests}}</td><td class="num">{{.Failed}}</td><td class="num">{{ms .TTFTP50MS}}</td><td class="num">{{ms .TTFTP95MS}}</td><td class="num">{{ms .TTFTP99MS}}</td><td class="num">{{ms .DurationP95MS}}</td><td class="num">{{float1 .TokensPerSP50}}</td></tr>{{end}}
</table></div>{{end}}
<h3>Cost</h3>
{{with .Cost}}{{if .USDPer1kRequests}}<p><strong>{{usd (deref .USDPer1kRequests)}}</strong> per 1,000 requests{{if .Estimate}} (estimate){{end}}, from the rate sheet {{with .PricingSheet}}<code>{{.}}</code> {{end}}as of {{.PricingAsOf}}{{with .PricingSource}}, source: {{.}}{{end}}.{{with .Basis}} {{.}}{{end}}</p>
{{else}}<p>Cost: <strong>not computed</strong>. {{.NotComputed}}</p>{{end}}
{{if .PricingStale}}<p class="banner small">The rate sheet is stale. {{.PricingWarning}}</p>{{else}}{{with .PricingWarning}}<p class="small muted">{{.}}</p>{{end}}{{end}}{{end}}
{{with .Spend}}<p>At the <strong>{{commas .MonthlyRequests}}</strong> requests a month you gave: <strong>{{usd .MonthlyUSD}} a month</strong> (measured cost × your volume; rates as of {{.PricingAsOf}}). This is spend, not a saving: no tool here measures what a fix would save.</p>{{end}}
<h3>Findings</h3>
{{range .Findings}}<div class="find"><strong>{{.Title}}</strong> <span class="st-{{.Status}}">{{.Status}}</span><span class="tag">{{.Ref}}</span><span class="tag">−{{.Impact}} grade points</span>{{with .Effort}}<span class="tag">effort {{.}}</span>{{end}}{{if .Preview}}<span class="tag">preview</span>{{end}}
{{with .Evidence}}<p class="small">{{.}}</p>{{end}}{{with .WhyItMatters}}<p class="small muted">{{.}}</p>{{end}}{{with .Fix}}<p class="small"><strong>Fix:</strong> {{.}}</p>{{end}}</div>
{{else}}<p class="muted">No findings.</p>{{end}}
{{range .Privacy}}<p class="small muted">{{.}}</p>{{end}}
</section>{{end}}

{{with .AgentSecurity}}<section id="agent-security">
<h2>Agent security</h2>
<p class="small muted">From Sievant: the MCP server configuration of the AI clients on this machine{{if not .ToolsInspected}}, read without starting any server{{end}}.</p>
<div class="scroll"><table>
<tr><th>Client</th><th>File</th><th>Scope</th><th class="num">Servers</th><th class="num">Not behind Sievant</th></tr>
{{range .Files}}<tr><td>{{.Client}}</td><td><code>{{.Path}}</code>{{with .Error}}<br><span class="small sev-high">{{.}}</span>{{end}}</td><td>{{.Scope}}</td><td class="num">{{.Servers}}</td><td class="num">{{.Unwrapped}}</td></tr>{{else}}<tr><td colspan="5" class="muted">No MCP client configuration found.</td></tr>{{end}}
</table></div>
<p class="small">Findings: {{index .Counts "critical"}} critical, {{index .Counts "high"}} high, {{index .Counts "medium"}} medium, {{index .Counts "low"}} low, {{index .Counts "info"}} info.</p>
{{range .Findings}}<div class="find"><span class="sev sev-{{.Severity}}">{{.Severity}}</span> <strong>{{.Title}}</strong><span class="tag">{{.Ref}}</span>{{if .DealRisk}}<span class="tag deal">deal risk</span>{{end}}
<p class="small muted">{{.Client}} · <code>{{.File}}</code>{{with .Server}} · server <code>{{.}}</code>{{end}}{{with .Where}} · <code>{{.}}</code>{{end}}</p>
{{with .Evidence}}<p class="small">Evidence (masked): <code>{{.}}</code></p>{{end}}{{with .Detail}}<p class="small muted">{{.}}</p>{{end}}{{with .Fix}}<p class="small"><strong>Fix:</strong> {{.}}</p>{{end}}</div>
{{else}}<p class="muted">No findings.</p>{{end}}
{{range .Notes}}<p class="small muted">{{.}}</p>{{end}}
{{with .Gateway}}<h3>Gateway coverage (sievant report, {{.Period}})</h3>
<p class="small">{{.Coverage}} · controls enforced {{.ControlsEnforced}} of {{.ControlsTotal}} · {{.Requests}} requests, {{.Blocked}} blocked.</p>
{{range .Gaps}}<div class="find"><span class="sev sev-{{.Severity}}">{{.Severity}}</span> <strong>{{.Title}}</strong>{{with .Fix}}<p class="small"><strong>Fix:</strong> {{.}}</p>{{end}}</div>{{end}}
<p class="small muted">{{.Disclaimer}}</p>{{end}}
</section>{{end}}

<section id="fix-plan">
<h2>30-day fix plan</h2>
<p class="small muted">Ordered by the ranking above. Each fix is the tool's own advice.</p>
{{range .FixPlan}}<div class="week"><strong>{{.Title}}</strong>
{{if .Items}}<ul>{{range .Items}}<li>{{.Title}} <span class="tag">{{source .Source}} {{.Ref}}</span>{{with .Effort}}<span class="tag">{{.}}</span>{{end}}{{with .Where}}<br><span class="small muted">{{.}}</span>{{end}}{{with .Fix}}<br><span class="small">{{.}}</span>{{end}}</li>{{end}}</ul>{{else}}<p class="small muted">Nothing this week.</p>{{end}}</div>
{{end}}
</section>

<section id="method">
<h2>How to read this report</h2>
<p class="small">{{.Ranking.Note}}</p>
<p class="small">{{.Privacy}}</p>
</section>

<footer>
{{with .CTA}}<div class="cta">Want a second pair of eyes? Book the <a href="{{.URL}}" rel="noreferrer">{{.Text}}</a>.</div>{{end}}
<p class="small muted">Generated by {{.Generator}} · schema {{.SchemaVersion}}</p>
</footer>
</main>
</body>
</html>
`
