<#
.SYNOPSIS
Exercises install.ps1's Invoke-HalotrcerNative under the PowerShell running
it (Windows PowerShell 5.1 or PowerShell 7), before any install channel runs.

.DESCRIPTION
One PASS line per assertion and one RESULT line ("native-ps<major>"), also
appended to $env:SMOKE_RESULTS:
  1. dot-sourcing install.ps1 leaves the caller's $repo alone;
  2. control (report-only): under 'Stop', the call operator with stderr
     redirected throws a NativeCommandError on Windows PowerShell 5.1 and
     PowerShell 7.0/7.1, the semantics the helper exists for;
  3. under 'Stop', Invoke-HalotrcerNative runs the same program without
     throwing, returns its exit code (3) and captures stdout and stderr apart;
  4. arguments reach the program intact through the Windows command line:
     spaces, quotes, trailing backslashes, backslashes before a quote, an
     empty argument.
#>
Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$major = $PSVersionTable.PSVersion.Major
$key = "native-ps$major"
$script:checks = 0
function Pass([string]$m) { $script:checks++; Write-Host "PASS ${key}: $m" }
function Fail([string]$m) { Write-Host "FAIL ${key}: $m"; Result "FAIL $m"; exit 1 }
function Result([string]$m) {
	Write-Host "RESULT $key $m"
	if ($env:SMOKE_RESULTS) { Add-Content -LiteralPath $env:SMOKE_RESULTS -Value "RESULT $key $m" -Encoding ASCII }
}

$installer = (Resolve-Path (Join-Path $PSScriptRoot '..\install.ps1')).Path

# 1. Dot-sourcing keeps the caller's variables.
$repo = 'sentinel'
. $installer
if ($repo -ne 'sentinel') { Fail "dot-sourcing install.ps1 changed `$repo to '$repo'" }
if (-not (Get-Command Invoke-HalotrcerNative -ErrorAction SilentlyContinue)) { Fail 'install.ps1 did not define Invoke-HalotrcerNative' }
Pass 'dot-sourcing install.ps1 defined Invoke-HalotrcerNative and left $repo alone'

# The child prints each argument it received, writes to stderr, exits 3.
$work = Join-Path ([System.IO.Path]::GetTempPath()) ('halotrcer-native-' + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$childScript = Join-Path $work 'child.ps1'
Set-Content -LiteralPath $childScript -Encoding ASCII -Value @'
$a = [Environment]::GetCommandLineArgs()
$i = [Array]::IndexOf($a, '-File')
for ($j = $i + 2; $j -lt $a.Length; $j++) { [Console]::Out.WriteLine('[' + $a[$j] + ']') }
[Console]::Error.WriteLine('progress on stderr')
exit 3
'@
if ($env:SystemRoot) {
	$childExe = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
} else {
	$childExe = (Get-Process -Id $PID).Path # off Windows (local runs): this pwsh
}
$fixed = @('-NoProfile', '-NonInteractive', '-File', $childScript)
$tricky = @('plain', 'a b', 'q"uote', 'trail\', 'back\\"slash', '')

try {
	# 2. Control: the call operator, stderr redirected, under 'Stop'.
	$minor = $PSVersionTable.PSVersion.Minor
	$stderrAborts = ($major -eq 5) -or ($major -eq 7 -and $minor -lt 2)
	$controlLog = Join-Path $work 'control.log'
	foreach ($form in @('*> file', '2>$null')) {
		$threw = $false
		try {
			if ($form -eq '*> file') {
				& $childExe @fixed 'x' *> $controlLog
			} else {
				$null = & $childExe @fixed 'x' 2>$null
			}
		} catch {
			$threw = $true
		}
		if ($threw) {
			Write-Host "PASS ${key}: control (report-only): on PowerShell $major.$minor, & with $form on a program that writes to stderr threw under Stop - the semantics the helper exists for"
		} else {
			Write-Host "INFO ${key}: control (report-only): on PowerShell $major.$minor, & with $form did not throw (expected to throw: $stderrAborts)"
		}
	}
	# The control's child exited 3; that must not leak out of this script.
	$global:LASTEXITCODE = 0

	# 3 and 4. The helper under 'Stop'.
	$r = Invoke-HalotrcerNative $childExe ($fixed + $tricky)
	Pass 'Invoke-HalotrcerNative returned under Stop although the program wrote to stderr'
	if ($r.ExitCode -ne 3) { Fail "exit code $($r.ExitCode), want 3" }
	Pass 'the exit code (3) came back unchanged'
	if ($r.StdErr.Trim() -ne 'progress on stderr') { Fail "stderr '$($r.StdErr.Trim())'" }
	Pass 'stderr was captured on its own'
	$got = @($r.StdOut -split "`r?`n" | Where-Object { $_ -ne '' })
	$want = @($tricky | ForEach-Object { "[$_]" })
	if (($got -join '|') -ne ($want -join '|')) { Fail "arguments arrived as $($got -join ' ') ; want $($want -join ' ')" }
	Pass "arguments arrived intact: $($want -join ' ')"
} finally {
	Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}
Result "PASS ($($script:checks) checks)"
exit 0
