<#
.SYNOPSIS
Runs Scoop's pinned installer against verified local archives, through the
installer's own dot-source entry point. Dot-sourced by smoke.ps1 (the Scoop
channel).

.DESCRIPTION
The installer has no parameter for where it downloads Scoop and the Main
bucket from: two script variables hold the moving master.zip URLs, and it
clones master with git when git is on PATH. Its documented downstream entry
point is to dot-source it with SCOOP_NOINSTALL=true (it then only defines its
functions and variables) and call Install-Scoop. Install-ScoopFromArchives
dot-sources it into its own scope, sets the two URL variables to file:///
URIs of the archives smoke.ps1 downloaded and hash-checked, answers "no" to
its git check, asserts all three, and calls Install-Scoop under 'Stop', as
the installer's own flow does. The installer's text is never edited (a text
patch broke it in Tokvera's smoke run 39027803219).
#>

function Install-ScoopFromArchives {
	param(
		[Parameter(Mandatory = $true)][string]$InstallerPath,
		[Parameter(Mandatory = $true)][string]$Directory,
		[Parameter(Mandatory = $true)][string]$CoreZip,
		[Parameter(Mandatory = $true)][string]$MainZip
	)
	# UriKind Absolute: the [System.Uri] cast reads a Unix path as relative.
	$CoreUri = (New-Object System.Uri($CoreZip, [System.UriKind]::Absolute)).AbsoluteUri
	$MainUri = (New-Object System.Uri($MainZip, [System.UriKind]::Absolute)).AbsoluteUri

	$noInstallBefore = $env:SCOOP_NOINSTALL
	$env:SCOOP_NOINSTALL = 'true'
	try {
		# Only the install flow writes output; a flow that ran shows here.
		$loaded = @(. $InstallerPath -RunAsAdmin -ScoopDir $Directory)
	} finally {
		$env:SCOOP_NOINSTALL = $noInstallBefore
	}
	if ($loaded.Count -gt 0) { throw "dot-sourcing the Scoop installer ran its install flow: $($loaded -join ' ')" }
	if (-not (Get-Command Install-Scoop -CommandType Function -ErrorAction SilentlyContinue)) {
		throw "the Scoop installer at $InstallerPath defined no Install-Scoop"
	}
	foreach ($name in @('SCOOP_PACKAGE_REPO', 'SCOOP_MAIN_BUCKET_REPO')) {
		if (-not (Get-Variable -Name $name -Scope 0 -ErrorAction SilentlyContinue)) {
			throw "the Scoop installer no longer sets `$$name"
		}
	}
	# The installer's own variables and function, overridden in this scope,
	# where Install-Scoop looks them up.
	$SCOOP_PACKAGE_REPO = $CoreUri
	$SCOOP_MAIN_BUCKET_REPO = $MainUri
	function Test-CommandAvailable {
		param([Parameter(Mandatory = $True, Position = 0)][String] $Command)
		# git would clone Scoop's master; everything else is the real check.
		if ($Command -eq 'git') { return $false }
		return [Boolean](Get-Command $Command -ErrorAction SilentlyContinue)
	}
	foreach ($want in @(@('SCOOP_PACKAGE_REPO', $CoreUri), @('SCOOP_MAIN_BUCKET_REPO', $MainUri), @('SCOOP_DIR', $Directory))) {
		$got = Get-Variable -Name $want[0] -Scope 0 -ValueOnly
		if ($got -ne $want[1]) { throw "`$$($want[0]) is '$got', want '$($want[1])'" }
	}
	if (Test-CommandAvailable 'git') { throw 'the Scoop installer would still use git' }

	$eapBefore = $ErrorActionPreference
	try {
		$ErrorActionPreference = 'Stop'
		Install-Scoop
	} finally {
		$ErrorActionPreference = $eapBefore
	}
}
