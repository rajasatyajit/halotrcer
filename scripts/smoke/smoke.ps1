<#
.SYNOPSIS
Smoke test for one Windows install channel (PLAN.md M4).

.DESCRIPTION
Run under the PowerShell being tested (Windows PowerShell 5.1 or PowerShell 7):
install.ps1 runs in this same host. -Dist is scripts/smoke/snapshot.sh's
output. The script starts testdata/mirror on loopback (the release mirror for
halotrcer, Tokvera and Sievant), installs all three through the channel,
checks the versions through `halotrcer doctor`, runs one `halotrcer check`,
checks the combined report (three formats, both sections, offline, no
<script>, no planted secret), and fails if install to report took more than
-Limit seconds. With the stand-in tools (Dist\TOOLS = stand-in, PLAN.md K1)
every RESULT line says so.

Channels:
  script  install.ps1 run as a script, -Version <tag> -Dir <dir>
  iex     install.ps1 piped to Invoke-Expression (latest, default
          directories); the session must survive with its preferences
  scoop   Scoop itself (installer, core and Main bucket pinned by commit and
          SHA-256), then a local rajasatyajit bucket with the three
          manifests pointed at the mirror: `scoop install
          rajasatyajit/halotrcer` installs its two dependencies
  winget  `winget validate` on the three projects' generated manifests, when
          winget is on the runner; halotrcer's lists both tools as
          dependencies

.EXAMPLE
pwsh -File scripts/smoke/smoke.ps1 -Channel script -Dist smoke-dist
#>
param(
	[Parameter(Mandatory = $true)][ValidateSet('script', 'iex', 'scoop', 'winget')][string]$Channel,
	[Parameter(Mandatory = $true)][string]$Dist,
	[int]$Limit = 300
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$smokeDist = (Resolve-Path $Dist).Path
$smokeLabel = ''
if ((Get-Content -LiteralPath (Join-Path $smokeDist 'TOOLS') -TotalCount 1 -ErrorAction SilentlyContinue) -eq 'stand-in') { $smokeLabel = ' (stand-in tools)' }

function Say([string]$m) { Write-Host "smoke[$Channel/PS $($PSVersionTable.PSVersion.Major)]: $m" }
function Fail([string]$m) { Say "FAIL: $m"; Result "FAIL $m"; exit 1 }
# One PASS line per assertion that holds, and exactly one RESULT line per run
# ("<channel>-ps<major>"), also appended to $env:SMOKE_RESULTS for
# scripts/smoke/check-results.sh.
$script:checks = 0
$script:resultKey = "$Channel-ps$($PSVersionTable.PSVersion.Major)"
function Pass([string]$m) { $script:checks++; Write-Host "PASS $($script:resultKey): $m" }
function Result([string]$m) {
	Write-Host "RESULT $($script:resultKey) $m$smokeLabel"
	if ($env:SMOKE_RESULTS) { Add-Content -LiteralPath $env:SMOKE_RESULTS -Value "RESULT $($script:resultKey) $m$smokeLabel" -Encoding ASCII }
}

$smokeRepo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$installer = Join-Path $smokeRepo 'scripts\install.ps1'
if (-not (Test-Path -LiteralPath $installer -PathType Leaf)) { throw "no installer at $installer" }
# install.ps1's Invoke-HalotrcerNative: every native program below runs
# through it (Windows PowerShell 5.1 aborts on a native program's first
# stderr line under 'Stop'). Dot-sourcing sets only Halotrcer-prefixed
# variables and its parameters (Only, Version, Dir, Global, DryRun, NoVerify,
# RequireCosign), none of which this script uses.
. $installer
$smokeTag = (Get-Content -LiteralPath (Join-Path $smokeDist 'TAG') -TotalCount 1).Trim()
$work = Join-Path ([System.IO.Path]::GetTempPath()) ('halotrcer-smoke-' + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$start = Get-Date

# Pinned third-party inputs for the Scoop channel: Scoop's installer and the
# core and Main bucket archives it would otherwise fetch from moving
# master.zip URLs (the pins Tokvera's smoke.ps1 records, each checked there
# against its publisher).
$scoopPins = @{
	Installer = @{ Url = 'https://raw.githubusercontent.com/ScoopInstaller/Install/1e2f334083d609986d8c8bc9e31ae8e87c39fab4/install.ps1'; Sha256 = '94f983b190438311e006b957db7c8422709e0ba62a6c2ac04e278164108f2512' }
	Core      = @{ Url = 'https://github.com/ScoopInstaller/Scoop/archive/e6aa3b366bdee8ed138c1e0f7b85192ebdd35d0f.zip'; Sha256 = 'fa96b15950f3726d2c9a35493e192eefe56d2eddba718e8121c32f7a19257a0a' }
	Main      = @{ Url = 'https://github.com/ScoopInstaller/Main/archive/bcff7c46a4ad0a7765b9f61260ab75440a3e69ea.zip'; Sha256 = '79fca73d0a4616a4c9ee8f36c9a1b438b6e65c24ab66580584282de69a71901c' }
}
# The winget installed when the runner's predates the v1.12 manifest schemas
# (winget.ps1): v1.29.380, the pin Tokvera's smoke.ps1 records (GitHub's
# asset digest and Microsoft's .txt beside it agree).
$wingetPin = @{
	Version = '1.29.380'
	Bundle  = @{ Url = 'https://github.com/microsoft/winget-cli/releases/download/v1.29.380/Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle'; Sha256 = '65dea9c01ce08ee7b763366b27c0e651f97db857c11ca9b9c301826c10092f2e' }
	Deps    = @{ Url = 'https://github.com/microsoft/winget-cli/releases/download/v1.29.380/DesktopAppInstaller_Dependencies.zip'; Sha256 = 'ba875afe9d190f61218985ac0292a99d1db710bf93e13c68944ca9d89f0d82d1' }
}

function Get-Pinned([hashtable]$pin, [string]$out) {
	[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor [System.Net.SecurityProtocolType]::Tls12
	Invoke-WebRequest -Uri $pin.Url -OutFile $out -UseBasicParsing
	$got = (Get-FileHash -LiteralPath $out -Algorithm SHA256).Hash.ToLowerInvariant()
	if ($got -ne $pin.Sha256) { Fail "sha256 of $($pin.Url) is $got, want $($pin.Sha256)" }
}

# The one tag the snapshot holds for a project, and its release directory.
function Get-ReleaseDir([string]$Name) {
	$d = @(Get-ChildItem -LiteralPath (Join-Path $smokeDist "releases\$Name") -Directory)
	if ($d.Count -ne 1) { Fail "want one $Name release in the snapshot, found $($d.Count)" }
	return $d[0]
}

$arch = 'amd64'
try {
	if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -eq 'Arm64') { $arch = 'arm64' }
} catch {
	Write-Verbose 'RuntimeInformation unavailable; assuming amd64'
}

$mirrorProc = $null
try {
	# --- The world: the mirror ----------------------------------------------------
	$urlFile = Join-Path $work 'url'
	$mirrorProc = Start-Process -FilePath (Join-Path $smokeDist "mirror-windows-$arch.exe") -PassThru -NoNewWindow `
		-ArgumentList @('--dist', $smokeDist, '--url-file', $urlFile, '--latest', "halotrcer=$smokeTag") `
		-RedirectStandardOutput (Join-Path $work 'mirror.out') -RedirectStandardError (Join-Path $work 'mirror.err')
	$deadline = (Get-Date).AddSeconds(15)
	while (-not (Test-Path -LiteralPath $urlFile) -or -not (Get-Content -LiteralPath $urlFile -ErrorAction SilentlyContinue)) {
		if ((Get-Date) -gt $deadline) { Fail 'the mirror did not start' }
		Start-Sleep -Milliseconds 100
	}
	$base = (Get-Content -LiteralPath $urlFile -TotalCount 1).Trim()
	$env:HALOTRCER_INSTALL_DOWNLOAD_BASE = "$base/dl"
	$env:HALOTRCER_INSTALL_API_BASE = "$base/api"
	Say "mirror at $base (halotrcer $smokeTag$smokeLabel)"

	# --- Install through the channel ------------------------------------------------
	$bin = $null
	$binDirs = @()
	switch ($Channel) {
		'script' {
			$d = Join-Path $work 'bin'
			& $installer -Version $smokeTag -Dir $d
			if ($LASTEXITCODE -ne 0) { Fail "install.ps1 exited $LASTEXITCODE" }
			Pass 'install.ps1 -Version exited 0'
			$bin = Join-Path $d 'halotrcer.exe'
			$binDirs = @($d)
		}
		'iex' {
			$env:LOCALAPPDATA = Join-Path $work 'localappdata'
			$eapBefore = $ErrorActionPreference
			Get-Content -Raw -LiteralPath $installer | Invoke-Expression
			if ($LASTEXITCODE -ne 0) { Fail "install.ps1 through iex set LASTEXITCODE $LASTEXITCODE" }
			if ($ErrorActionPreference -ne $eapBefore) { Fail 'install.ps1 through iex changed the session''s ErrorActionPreference' }
			Pass 'install.ps1 through iex exited 0 and left the session''s preferences unchanged'
			# Each where its own installer puts it.
			$binDirs = @('halotrcer', 'tokvera', 'sievant' | ForEach-Object { Join-Path $env:LOCALAPPDATA "Programs\$_" })
			$bin = Join-Path $binDirs[0] 'halotrcer.exe'
		}
		'scoop' {
			$sdir = Join-Path $work 'scoop-src'
			New-Item -ItemType Directory -Path $sdir | Out-Null
			Get-Pinned $scoopPins.Installer "$sdir\install.ps1"
			Get-Pinned $scoopPins.Core "$sdir\scoop.zip"
			Get-Pinned $scoopPins.Main "$sdir\main.zip"
			. (Join-Path $PSScriptRoot 'scoop-local.ps1')
			$scoopDir = Join-Path $work 'scoop'
			Install-ScoopFromArchives -InstallerPath "$sdir\install.ps1" -Directory $scoopDir -CoreZip "$sdir\scoop.zip" -MainZip "$sdir\main.zip"
			if (-not (Test-Path -LiteralPath "$scoopDir\shims\scoop.ps1")) { Fail 'the Scoop installer did not install Scoop' }
			Pass 'Install-Scoop installed Scoop from the verified local archives (installer unedited, git not used)'
			$env:PATH = "$scoopDir\shims;$env:PATH"
			# A local bucket under the real bucket's name, so halotrcer's
			# `depends` (rajasatyajit/tokvera, rajasatyajit/sievant) resolve
			# to it; each manifest's downloads pointed at the mirror (the same
			# files and hashes the manifests check).
			$bucket = Join-Path $scoopDir 'buckets\rajasatyajit\bucket'
			New-Item -ItemType Directory -Path $bucket -Force | Out-Null
			foreach ($p in @('halotrcer', 'tokvera', 'sievant')) {
				$t = (Get-ReleaseDir $p).Name
				$manifest = (Get-Content -Raw -LiteralPath (Join-Path $smokeDist "scoop\$p.json")) -replace "https://github\.com/rajasatyajit/$p/releases/download/[^/`"]+/", "$base/dl/$p/releases/download/$t/"
				if ($manifest -notmatch [regex]::Escape("$base/dl/$p/releases/download/$t/")) { Fail "could not point the $p Scoop manifest at the mirror" }
				Set-Content -LiteralPath (Join-Path $bucket "$p.json") -Value $manifest -Encoding UTF8
			}
			# Scoop's scripts call native programs themselves; under 'Stop' on
			# 5.1 their stderr would abort this script, so `scoop install`
			# runs under 'Continue' and is judged by its exit code.
			$ErrorActionPreference = 'Continue'
			& "$scoopDir\shims\scoop.ps1" install rajasatyajit/halotrcer
			$scoopExit = $LASTEXITCODE
			$ErrorActionPreference = 'Stop'
			if ($scoopExit -ne 0) { Fail "scoop install exited $scoopExit" }
			foreach ($p in @('tokvera', 'sievant')) {
				if (-not (Test-Path -LiteralPath "$scoopDir\shims\$p.exe")) { Fail "Scoop did not install $p with halotrcer" }
			}
			Pass 'scoop install rajasatyajit/halotrcer installed it and its two dependencies from the local bucket'
			$binDirs = @("$scoopDir\shims")
			$bin = "$scoopDir\shims\halotrcer.exe"
		}
		'winget' {
			$manifest = Get-Content -Raw -LiteralPath (Join-Path $smokeDist 'winget\Halotrcer.Halotrcer\Halotrcer.Halotrcer.installer.yaml')
			foreach ($id in @('Halotrcer.Tokvera', 'Halotrcer.Sievant')) {
				if ($manifest -notmatch "PackageIdentifier: $([regex]::Escape($id))") { Fail "halotrcer's winget manifest does not depend on $id" }
			}
			Pass 'halotrcer''s winget manifest lists Halotrcer.Tokvera and Halotrcer.Sievant as dependencies'
			$w = Get-Command winget -ErrorAction SilentlyContinue
			if (-not $w) {
				Result 'SKIP winget is not on this runner (the dependencies are checked above)'
				exit 0
			}
			. (Join-Path $PSScriptRoot 'winget.ps1')
			$wingetExe = $w.Source
			$have = ConvertFrom-WingetVersion (Invoke-HalotrcerNative $wingetExe @('--version')).StdOut
			Say "winget on the runner: v$have"
			if ($have -lt $WingetMinimum) {
				$wdir = Join-Path $work 'winget-src'
				New-Item -ItemType Directory -Path $wdir | Out-Null
				Get-Pinned $wingetPin.Bundle "$wdir\winget.msixbundle"
				Get-Pinned $wingetPin.Deps "$wdir\deps.zip"
				Install-WingetBundle "$wdir\winget.msixbundle" "$wdir\deps.zip" $arch $wdir
				$alias = Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps\winget.exe'
				if (Test-Path -LiteralPath $alias) { $wingetExe = $alias }
				$was = $have
				$have = ConvertFrom-WingetVersion (Invoke-HalotrcerNative $wingetExe @('--version')).StdOut
				if ($have -ne [version]$wingetPin.Version) { Fail "installed the pinned winget v$($wingetPin.Version), but $wingetExe reports v$have" }
				Pass "the runner's winget v$was predates the v1.12 manifest schemas; installed the pinned, verified v$have"
			}
			foreach ($id in @('Halotrcer.Halotrcer', 'Halotrcer.Tokvera', 'Halotrcer.Sievant')) {
				$r = Invoke-HalotrcerNative $wingetExe @('validate', '--manifest', (Join-Path $smokeDist "winget\$id"))
				if ($r.ExitCode -ne 0) { Fail "winget v$have validate $id exited $($r.ExitCode): $($r.StdOut) $($r.StdErr)" }
			}
			Pass "winget v$have validate accepted the three projects' manifests with no warnings"
			Result "PASS ($($script:checks) checks)"
			exit 0
		}
	}

	# --- The three binaries run, at the right versions --------------------------------
	if (-not (Test-Path -LiteralPath $bin)) { Fail "no halotrcer at $bin" }
	$env:PATH = (($binDirs + @($env:PATH)) -join ';')
	$v = Invoke-HalotrcerNative $bin @('version')
	$got = ($v.StdOut -split "`r?`n")[0]
	if ($v.ExitCode -ne 0 -or -not $got.StartsWith("halotrcer $smokeTag ")) { Fail "installed '$got' (exit $($v.ExitCode)), want halotrcer $smokeTag" }
	Pass "installed $got"
	$doc = Invoke-HalotrcerNative $bin @('doctor')
	foreach ($p in @('tokvera', 'sievant')) {
		if ($doc.StdOut -notmatch "(?m)^\[ok\] ${p}: v0\.2\.0 ") { Fail "doctor: $p is not v0.2.0 inside the matrix: $($doc.StdOut)" }
	}
	Pass 'doctor: tokvera and sievant v0.2.0, inside the compatibility matrix'

	# --- One check, and the combined report --------------------------------------------
	# A planted API key: its value must reach no output (the name goes to Tokvera).
	$canary = "sk-smoke-canary-$PID-not-a-key"
	$env:SMOKE_CANARY_KEY = $canary
	$out = Join-Path $work 'report'
	$c = Invoke-HalotrcerNative $bin @('check', '--target', "$base/v1", '--yes', '--no-project', '--api-key-env', 'SMOKE_CANARY_KEY',
		'--mcp-config', (Join-Path $smokeRepo 'testdata\mcp\smoke-mcp.json'), '--out', $out)
	if ($c.ExitCode -ne 0) { Fail "check exited $($c.ExitCode): $($c.StdErr)" }
	foreach ($f in @('report.html', 'report.pdf', 'report.json')) {
		$p = Join-Path $out $f
		if (-not (Test-Path -LiteralPath $p) -or (Get-Item -LiteralPath $p).Length -eq 0) { Fail "check wrote no $f" }
	}
	$html = Get-Content -Raw -LiteralPath (Join-Path $out 'report.html')
	if ($html -notmatch 'Cost &amp; performance') { Fail 'the report has no Cost & performance section' }
	if ($html -notmatch 'Agent security') { Fail 'the report has no Agent security section' }
	if ($html -match '(?i)<script') { Fail 'the report has a <script>' }
	if ($html -match '(?i)(src|srcset)=["'']?https?://|url\(["'']?https?://|<link[^>]*href=["'']?https?://') { Fail 'the report loads an external resource (it must open offline)' }
	$texts = @($html, (Get-Content -Raw -LiteralPath (Join-Path $out 'report.json')), [System.IO.File]::ReadAllText((Join-Path $out 'report.pdf')), $c.StdOut, $c.StdErr)
	foreach ($s in @($canary, 'smoke-canary-in-url-0000', 'smoke-canary-env-value-0000')) {
		foreach ($t in $texts) {
			if ($t -and $t.Contains($s)) { Fail 'a planted secret reached the report or the output' }
		}
	}
	Pass 'check wrote report.html, .pdf and .json: both sections, self-contained, no planted secret'

	$elapsed = [int]((Get-Date) - $start).TotalSeconds
	if ($elapsed -gt $Limit) { Fail "install to report took ${elapsed}s, over the ${Limit}s limit" }
	Pass "install to report in ${elapsed}s (limit ${Limit}s)"
	Result "PASS ($($script:checks) checks)"
} catch {
	# Any terminating error a step did not expect becomes this channel's FAIL
	# with its reason; uncaught, it would end the script with exit code 0 and
	# no RESULT line (Tokvera smoke run 39027803219).
	Fail "unexpected error: $($_.Exception.Message) (at $($_.InvocationInfo.ScriptName):$($_.InvocationInfo.ScriptLineNumber))"
} finally {
	if ($mirrorProc -and -not $mirrorProc.HasExited) { Stop-Process -Id $mirrorProc.Id -Force -ErrorAction SilentlyContinue }
	Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}
# Explicit: the result is the RESULT line above, never a native exit code a
# step happened to leave in $LASTEXITCODE.
exit 0
