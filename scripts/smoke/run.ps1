<#
.SYNOPSIS
Runs one Windows smoke script in a child process of the same PowerShell, and
never stops the job (PLAN.md M4).

.DESCRIPTION
The child is this host's own executable (powershell.exe for 5.1, pwsh for 7)
with -File, so every step starts from a clean session and its exit code is
the script's own: nothing an earlier step left in $LASTEXITCODE or the error
stream decides this one. A FAIL line is added to $env:SMOKE_RESULTS when the
child reported no RESULT for -Key, exited non-zero without a FAIL, or wrote
to its error stream without a FAIL (a non-terminating error leaves the exit
code 0). Always exits 0; scripts/smoke/check-results.sh decides the job.

.EXAMPLE
./scripts/smoke/run.ps1 -Key script-ps5 -Script scripts/smoke/smoke.ps1 -Arguments @('-Channel', 'script', '-Dist', 'smoke-dist')
#>
param(
	[Parameter(Mandatory = $true)][string]$Key,
	[Parameter(Mandatory = $true)][string]$Script,
	[string[]]$Arguments = @()
)
$ErrorActionPreference = 'Stop'
$results = $env:SMOKE_RESULTS
if (-not $results) { throw 'set SMOKE_RESULTS' }
if (-not (Test-Path -LiteralPath $results)) { New-Item -ItemType File -Path $results | Out-Null }

# install.ps1's Invoke-HalotrcerNative quotes each argument for the Windows
# command line and keeps the child's stderr out of this session's error stream.
. (Join-Path $PSScriptRoot '..\install.ps1')
$exe = [System.Diagnostics.Process]::GetCurrentProcess().MainModule.FileName
$argList = @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', (Resolve-Path $Script).Path) + $Arguments
$exitCode = $null
$stderr = ''
try {
	$r = Invoke-HalotrcerNative $exe $argList
	$exitCode = $r.ExitCode
	if ($r.StdOut) { Write-Host $r.StdOut.TrimEnd() }
	if ($r.StdErr) { $stderr = $r.StdErr; Write-Host $stderr.TrimEnd() }
} catch {
	Write-Host "run.ps1: could not start $Script under ${exe}: $($_.Exception.Message)"
	$exitCode = -1
}
# The RESULT line decides: a script killed by an uncaught terminating error
# can still exit 0 under -File (Tokvera smoke run 39027803219).
$k = [regex]::Escape($Key)
$reported = Select-String -LiteralPath $results -Pattern "^RESULT $k " -Quiet
$failed = Select-String -LiteralPath $results -Pattern "^RESULT $k FAIL" -Quiet
if (-not $reported) {
	Add-Content -LiteralPath $results -Value "RESULT $Key FAIL exited $exitCode without reporting a result" -Encoding ASCII
} elseif ($exitCode -ne 0 -and -not $failed) {
	Add-Content -LiteralPath $results -Value "RESULT $Key FAIL exited $exitCode after reporting a result" -Encoding ASCII
}
# Every native program the smoke scripts run goes through
# Invoke-HalotrcerNative, which captures its stderr, so a clean child writes
# nothing to its own stderr; anything there is a non-terminating error.
$wroteErrors = $stderr.Trim() -ne ''
if ($wroteErrors -and $reported -and -not $failed) {
	# ANSI colour codes out ([char]27: Windows PowerShell 5.1 has no `e).
	$first = (($stderr -replace "$([char]27)\[[0-9;]*m", '').Trim() -split "`r?`n")[0].Trim()
	if ($first.Length -gt 200) { $first = $first.Substring(0, 200) + '...' }
	Add-Content -LiteralPath $results -Value "RESULT $Key FAIL wrote to the error stream: $first" -Encoding ASCII
}
if (-not $reported -or ($exitCode -ne 0) -or $wroteErrors) {
	Write-Host "::error::smoke step $Key failed (exit $exitCode); the job's last step reports it"
}
exit 0
