<#
.SYNOPSIS
winget helpers for smoke.ps1's winget channel, dot-sourced by smoke.ps1.

.DESCRIPTION
GoReleaser v2.18.3 writes ManifestVersion 1.12.0. A winget client older than
v1.12.350, the first release with the v1.12 schemas, reports every schema
header of such a manifest as "does not match the expected pattern" (Tokvera
smoke run 39027803219, winget-cli #5687), so the channel validates with a
winget that carries them, and installs a pinned one when the runner's is
older.
#>

# The first winget release whose bundled schemas include v1.12.0.
$WingetMinimum = [version]'1.12.350'

# ConvertFrom-WingetVersion reads `winget --version` output ("v1.11.510",
# "v1.30.140-preview") as a [version]; it throws on anything else.
function ConvertFrom-WingetVersion([string]$Text) {
	$m = [regex]::Match($Text.Trim(), '^v?(\d+\.\d+\.\d+)(-[0-9A-Za-z.-]+)?$')
	if (-not $m.Success) { throw "winget --version printed '$($Text.Trim())', not a version" }
	return [version]$m.Groups[1].Value
}

# Install-WingetBundle adds a verified App Installer bundle and the dependency
# packages for $Arch (amd64 or arm64) from the release's
# DesktopAppInstaller_Dependencies.zip, under Windows PowerShell, where the
# Appx module is native.
function Install-WingetBundle([string]$Bundle, [string]$DepsZip, [string]$Arch, [string]$Work) {
	$depArch = @{ amd64 = 'x64'; arm64 = 'arm64' }[$Arch]
	if (-not $depArch) { throw "no winget dependencies for architecture $Arch" }
	$depDir = Join-Path $Work 'winget-deps'
	Expand-Archive -LiteralPath $DepsZip -DestinationPath $depDir -Force
	$deps = @(Get-ChildItem -LiteralPath (Join-Path $depDir $depArch) -Filter '*.appx' | ForEach-Object { $_.FullName })
	if ($deps.Count -eq 0) { throw "DesktopAppInstaller_Dependencies.zip has no $depArch packages" }
	foreach ($p in @($Bundle) + $deps) {
		if ($p.Contains("'")) { throw "a winget package path contains a quote: $p" }
	}
	$command = "`$ErrorActionPreference = 'Stop'; Add-AppxPackage -Path '$Bundle' -DependencyPath @('" + ($deps -join "', '") + "')"
	$ps51 = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
	$r = Invoke-HalotrcerNative $ps51 @('-NoProfile', '-NonInteractive', '-Command', $command)
	if ($r.ExitCode -ne 0) { throw "Add-AppxPackage exited $($r.ExitCode): $($r.StdErr.Trim()) $($r.StdOut.Trim())" }
}
