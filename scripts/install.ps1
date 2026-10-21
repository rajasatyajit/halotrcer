<#
.SYNOPSIS
Install halotrcer, Tokvera and Sievant on Windows (https://github.com/rajasatyajit/halotrcer).

.DESCRIPTION
What it does, in order (every step is commented below):
  1. detects the CPU (an x64 PowerShell on Windows on Arm still gets the
     native arm64 build: the OS architecture decides, not the process);
  2. resolves the halotrcer version (the latest published release unless
     -Version);
  3. downloads halotrcer's release zip and checksums.txt from GitHub, unless
     that version is already installed (safe to run again);
  4. verifies checksums.txt's cosign signature when cosign is installed
     (required with -RequireCosign), then the zip's SHA-256;
  5. installs halotrcer.exe atomically (with -Only, it is kept in a scratch
     directory instead and removed at the end);
  6. has that halotrcer install Tokvera and Sievant, each from its own
     release, verified the same way against its own release workflow's
     identity, at the newest stable version inside that halotrcer's
     compatibility matrix, into the directory each tool's own installer
     uses (or -Dir). A tool already installed elsewhere on PATH (Scoop,
     winget, go install) is left alone;
  7. prints a PATH hint if needed, and the next command.

It contacts only GitHub (api.github.com for versions, github.com for the
release files), never asks for credentials, and with -DryRun contacts nothing.
It never edits PATH itself, and never elevates: -Global needs a PowerShell
you started as administrator. Runs on Windows PowerShell 5.1 and PowerShell 7.

Exit codes (when run as a file): 0 installed, already installed or dry run;
1 failure; 2 usage error.

.EXAMPLE
irm https://www.halotrcer.com/install.ps1 | iex

.EXAMPLE
& ([scriptblock]::Create((irm https://www.halotrcer.com/install.ps1))) -Only tokvera

.EXAMPLE
powershell -ExecutionPolicy Bypass -File install.ps1 -DryRun
#>
[CmdletBinding()]
param(
	# Install only tokvera or only sievant (halotrcer is used to install it,
	# then removed).
	[string]$Only = "",
	# The halotrcer release (e.g. v0.2.0); default: the latest published
	# release. The tools' versions come from that release's compatibility
	# matrix.
	[string]$Version = "",
	# Install all of them into this directory (default: each into
	# $env:LOCALAPPDATA\Programs\<name>, where each tool's own installer puts it).
	[string]$Dir = "",
	# Install each into $env:ProgramFiles\<name> (needs an elevated PowerShell).
	[switch]$Global,
	# Print what would happen; download and change nothing.
	[switch]$DryRun,
	# Skip the SHA-256 and signature checks (not recommended).
	[switch]$NoVerify,
	# Fail unless cosign is installed and every signature verifies.
	[switch]$RequireCosign
)

# --- Constants -----------------------------------------------------------------

# Script-scope names all start with Halotrcer: a smoke script dot-sources this
# file for Invoke-HalotrcerNative, and PowerShell variable names are
# case-insensitive, so a plain $script:Repo once overwrote a caller's $repo in
# Tokvera's installer (TestPS1DotSourcingKeepsTheCallersVariables).

$script:HalotrcerRepo = 'rajasatyajit/halotrcer'
# The identity cosign must see on the checksum file's signature: the release
# workflow of this repository, running on a version tag.
$script:HalotrcerCosignIdentityRegexp = '^https://github\.com/rajasatyajit/halotrcer/\.github/workflows/release\.yml@refs/tags/v'
$script:HalotrcerCosignOidcIssuer = 'https://token.actions.githubusercontent.com'
$script:HalotrcerVersionPattern = '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
$script:HalotrcerTools = @('tokvera', 'sievant')

# A usage error (exit 2), as distinct from a failure (exit 1).
class HalotrcerUsageError : System.Exception {
	HalotrcerUsageError([string]$message) : base($message) {}
}

function Write-Step([string]$Message) {
	# Progress goes to the host, never the output stream.
	Write-Host "halotrcer-install: $Message"
}

# --- Seams (small functions so the steps can be tested off Windows) ---------------

function Test-HalotrcerWindows {
	return [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
}

# The OS architecture, not the process's: an x64 PowerShell emulated on Windows
# on Arm reports AMD64 in PROCESSOR_ARCHITECTURE but Arm64 here.
function Get-HalotrcerArch {
	$name = $null
	try {
		$name = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
	} catch {
		# Older .NET without RuntimeInformation: a 32-bit process on 64-bit
		# Windows sees the real CPU in PROCESSOR_ARCHITEW6432.
		$name = $env:PROCESSOR_ARCHITEW6432
		if (-not $name) { $name = $env:PROCESSOR_ARCHITECTURE }
	}
	switch ($name) {
		'X64' { return 'amd64' }
		'AMD64' { return 'amd64' }
		'Arm64' { return 'arm64' }
		default { throw "unsupported CPU: $name (release builds exist for amd64 and arm64)" }
	}
}

# ConvertTo-HalotrcerArgument quotes one argument for a Windows command line
# (the rules CommandLineToArgvW and the .NET runtime parse back): needed
# because Windows PowerShell 5.1 runs on .NET Framework, whose ProcessStartInfo
# has no ArgumentList, only one Arguments string.
function ConvertTo-HalotrcerArgument([string]$Value) {
	if ($Value -ne '' -and $Value -notmatch '[\s"]') { return $Value }
	$sb = New-Object System.Text.StringBuilder
	[void]$sb.Append('"')
	$backslashes = 0
	foreach ($ch in $Value.ToCharArray()) {
		if ($ch -eq [char]'\') {
			$backslashes++
		} elseif ($ch -eq '"') {
			[void]$sb.Append([char]'\', 2 * $backslashes + 1).Append('"')
			$backslashes = 0
		} else {
			if ($backslashes -gt 0) { [void]$sb.Append([char]'\', $backslashes) }
			[void]$sb.Append($ch)
			$backslashes = 0
		}
	}
	if ($backslashes -gt 0) { [void]$sb.Append([char]'\', 2 * $backslashes) }
	[void]$sb.Append('"')
	return $sb.ToString()
}

# Invoke-HalotrcerNative runs a native program and returns its exit code,
# stdout and stderr, captured separately. Success is the exit code alone.
# Windows PowerShell 5.1 turns every line a native program writes to stderr
# into an error record, and under $ErrorActionPreference = 'Stop' the first one
# aborts the script - even with 2>$null, even when the program succeeded
# (cosign prints "Verified OK" to stderr). Running the program through
# System.Diagnostics.Process keeps its stderr away from PowerShell's error
# stream on 5.1 and 7 alike. Every native call in this script goes through
# here (a test enforces it). $Environment sets variables for the child only,
# never for the caller's session.
function Invoke-HalotrcerNative([string]$FilePath, [string[]]$Arguments = @(), [hashtable]$Environment = @{}) {
	$psi = New-Object System.Diagnostics.ProcessStartInfo
	$psi.FileName = $FilePath
	$psi.Arguments = (@($Arguments | ForEach-Object { ConvertTo-HalotrcerArgument $_ }) -join ' ')
	$psi.UseShellExecute = $false
	$psi.RedirectStandardOutput = $true
	$psi.RedirectStandardError = $true
	$psi.CreateNoWindow = $true
	foreach ($k in $Environment.Keys) { $psi.EnvironmentVariables[$k] = [string]$Environment[$k] }
	$p = New-Object System.Diagnostics.Process
	$p.StartInfo = $psi
	[void]$p.Start()
	# Both pipes are drained concurrently, so neither can fill and stall the
	# child.
	$stderrTask = $p.StandardError.ReadToEndAsync()
	$stdout = $p.StandardOutput.ReadToEnd()
	$p.WaitForExit()
	return [pscustomobject]@{ ExitCode = $p.ExitCode; StdOut = $stdout; StdErr = $stderrTask.Result }
}

# The first line `halotrcer version` prints: "halotrcer vX.Y.Z (commit ...)".
function Get-HalotrcerVersionLine([string]$Exe) {
	try {
		$r = Invoke-HalotrcerNative $Exe @('version')
	} catch {
		return ''
	}
	if ($r.ExitCode -ne 0 -or -not $r.StdOut) { return '' }
	return ($r.StdOut -split "`r?`n")[0]
}

function Test-HalotrcerElevated {
	$id = [System.Security.Principal.WindowsIdentity]::GetCurrent()
	return (New-Object System.Security.Principal.WindowsPrincipal($id)).IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)
}

# Where a tool is installed outside this run's directories (Scoop, winget,
# go install), or $null.
function Find-HalotrcerTool([string]$Name) {
	$c = Get-Command "$Name.exe" -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
	if ($c) { return $c.Source }
	return $null
}

# Has halotrcer install one tool: `halotrcer install TOOL --via direct --dir
# DIR [--require-cosign|--no-verify]`, with DIR first on the child's PATH so a
# tool installed there before is upgraded in place.
function Invoke-HalotrcerToolInstall([string]$Exe, [string[]]$Arguments, [string]$ToolDir) {
	return Invoke-HalotrcerNative $Exe $Arguments @{ Path = "$ToolDir;$env:Path" }
}

# An overridden endpoint (a mirror or a test server) must be https, or plain
# http to this machine only.
function Assert-HalotrcerEndpoint([string]$Url, [string]$Name) {
	$uri = $null
	if (-not [System.Uri]::TryCreate($Url, [System.UriKind]::Absolute, [ref]$uri)) {
		throw [HalotrcerUsageError]::new("$Name must be an https URL (or http to a loopback host), got: $Url")
	}
	$loopback = @('127.0.0.1', 'localhost', '[::1]', '::1') -contains $uri.Host
	if (-not ($uri.Scheme -eq 'https' -or ($uri.Scheme -eq 'http' -and $loopback))) {
		throw [HalotrcerUsageError]::new("$Name must be an https URL (or http to a loopback host), got: $Url")
	}
}

function Get-HalotrcerFile([string]$Url, [string]$OutFile) {
	Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
}

# The directory one program goes into: -Dir for all of them, else the
# program's own (where each tool's own install.ps1 puts it).
function Get-HalotrcerDir([string]$Name, [string]$Dir, [switch]$Global) {
	if ($Dir) { return $Dir }
	if ($Global) { return (Join-Path $env:ProgramFiles $Name) }
	return (Join-Path $env:LOCALAPPDATA "Programs\$Name")
}

# --- The installer ---------------------------------------------------------------

function Install-Halotrcer {
	param(
		[string]$Only = "",
		[string]$Version = "",
		[string]$Dir = "",
		[switch]$Global,
		[switch]$DryRun,
		[switch]$NoVerify,
		[switch]$RequireCosign
	)

	# Set inside the function so `irm | iex`, which runs in the caller's scope,
	# leaves the user's session preferences alone. PowerShell scopes are
	# dynamic, so the helpers called from here inherit them.
	Set-StrictMode -Version 2.0
	$ErrorActionPreference = 'Stop'
	# Invoke-WebRequest is many times slower with its progress bar on 5.1.
	$ProgressPreference = 'SilentlyContinue'
	# Windows PowerShell 5.1 on older .NET may not offer TLS 1.2 by default;
	# GitHub requires it. This adds TLS 1.2 to what is allowed, removing nothing.
	try {
		[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor [System.Net.SecurityProtocolType]::Tls12
	} catch {
		Write-Verbose 'could not adjust SecurityProtocol; continuing with the defaults'
	}

	# Step 0: refuse contradictory or malformed options before anything runs.
	if ($Only -and $script:HalotrcerTools -notcontains $Only) { throw [HalotrcerUsageError]::new("-Only must be tokvera or sievant, got: $Only") }
	if ($Global -and $Dir) { throw [HalotrcerUsageError]::new('-Dir and -Global are mutually exclusive') }
	if ($NoVerify -and $RequireCosign) { throw [HalotrcerUsageError]::new('-NoVerify and -RequireCosign are mutually exclusive') }
	if ($Version) {
		# Accept "0.2.0" as well as "v0.2.0"; the release tags carry the v.
		if (-not $Version.StartsWith('v')) { $Version = "v$Version" }
		if ($Version -notmatch $script:HalotrcerVersionPattern) {
			throw [HalotrcerUsageError]::new("-Version must look like v1.2.3 or v1.2.3-rc.1, got: $Version")
		}
	}
	# The release files and the API for every repository of the owner;
	# halotrcer reads the same two variables for Tokvera and Sievant.
	$downloadBase = $env:HALOTRCER_INSTALL_DOWNLOAD_BASE
	if (-not $downloadBase) { $downloadBase = 'https://github.com/rajasatyajit' }
	$apiBase = $env:HALOTRCER_INSTALL_API_BASE
	if (-not $apiBase) { $apiBase = 'https://api.github.com/repos/rajasatyajit' }
	Assert-HalotrcerEndpoint $downloadBase 'HALOTRCER_INSTALL_DOWNLOAD_BASE'
	Assert-HalotrcerEndpoint $apiBase 'HALOTRCER_INSTALL_API_BASE'
	$downloadBase = $downloadBase.TrimEnd('/')
	$apiBase = $apiBase.TrimEnd('/')

	$tools = $script:HalotrcerTools
	$what = 'halotrcer, tokvera and sievant'
	if ($Only) {
		$tools = @($Only)
		$what = $Only
	}
	$verifyFlag = @()
	if ($NoVerify) { $verifyFlag = @('--no-verify') } elseif ($RequireCosign) { $verifyFlag = @('--require-cosign') }

	# Step 1: the platform. This is the Windows installer; install.sh covers
	# Linux and macOS.
	if (-not (Test-HalotrcerWindows)) {
		throw 'this is the Windows installer; on Linux and macOS use install.sh (see docs/install.md)'
	}
	$arch = Get-HalotrcerArch

	# Step 2: the destinations. -Global writes under Program Files, which needs
	# an elevated PowerShell; there is no silent elevation.
	if (-not $Dir -and -not $Global -and -not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set; pass -Dir' }
	$halDir = Get-HalotrcerDir 'halotrcer' $Dir -Global:$Global
	$target = Join-Path $halDir 'halotrcer.exe'

	# Dry run: print the plan, contact nothing.
	if ($DryRun) {
		if ($Version) {
			$shown = $Version
			$zip = "halotrcer_$($Version.Substring(1))_windows_$arch.zip"
		} else {
			$shown = "latest (resolved from $apiBase/halotrcer/releases/latest at install time)"
			$zip = "halotrcer_<version>_windows_$arch.zip"
		}
		Write-Step 'dry run: nothing will be downloaded or changed'
		Write-Step "installs:     $what"
		Write-Step "platform:     windows/$arch"
		Write-Step "halotrcer:    $shown"
		Write-Step "archive:      $downloadBase/halotrcer/releases/download/<tag>/$zip"
		if ($NoVerify) {
			Write-Step 'verify:       SKIPPED (-NoVerify)'
		} else {
			$req = ''
			if ($RequireCosign) { $req = ' (required)' }
			Write-Step "verify:       SHA-256 against checksums.txt; cosign signature of checksums.txt if cosign is installed$req"
		}
		if ($Only) {
			Write-Step 'halotrcer:    kept in a scratch directory, removed at the end'
		} else {
			Write-Step "install to:   $target"
		}
		foreach ($t in $tools) {
			$d = Get-HalotrcerDir $t $Dir -Global:$Global
			Write-Step "then:         halotrcer install $t --via direct --dir $d$(if ($verifyFlag) { ' ' + $verifyFlag[0] })"
		}
		Write-Step '              (the newest stable release in halotrcer''s compatibility matrix; a tool already on PATH is left alone)'
		return
	}

	if ($Global -and -not (Test-HalotrcerElevated)) {
		throw "-Global installs under $env:ProgramFiles, which needs an elevated PowerShell (Run as administrator); this script never elevates. Or pass -Dir"
	}

	# Step 3: cosign is optional unless -RequireCosign; checked before downloading.
	$cosign = $null
	if (-not $NoVerify) {
		$cosign = Get-Command cosign -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
		if (-not $cosign -and $RequireCosign) {
			throw '-RequireCosign: cosign is not installed (https://docs.sigstore.dev/cosign/system_config/installation/)'
		}
	}

	# A scratch directory for everything downloaded; removed on every exit path.
	$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("halotrcer-install-" + [System.Guid]::NewGuid().ToString('N'))
	New-Item -ItemType Directory -Path $tmp | Out-Null
	try {
		# Step 4: resolve the version: the latest PUBLISHED release (drafts are
		# not visible here).
		if (-not $Version) {
			try {
				$latest = Invoke-RestMethod -Uri "$apiBase/halotrcer/releases/latest" -UseBasicParsing
			} catch {
				throw "could not find the latest release at $apiBase/halotrcer/releases/latest; pass -Version"
			}
			$Version = [string]$latest.tag_name
			if ($Version -notmatch $script:HalotrcerVersionPattern) {
				throw "the latest release has an unexpected tag ($Version); pass -Version"
			}
		}
		Write-Step "installing $what for windows/$arch (halotrcer $Version)"

		# Step 5: use halotrcer if this version is already installed (idempotent).
		$hal = $null
		if (Test-Path -LiteralPath $target -PathType Leaf) {
			$line = Get-HalotrcerVersionLine $target
			if ($line -eq "halotrcer $Version" -or $line.StartsWith("halotrcer $Version ")) {
				Write-Step "halotrcer $Version is already installed at $target"
				$hal = $target
			}
		}

		if (-not $hal) {
			# Step 6: download the zip and the checksum file.
			$zip = "halotrcer_$($Version.Substring(1))_windows_$arch.zip"
			$base = "$downloadBase/halotrcer/releases/download/$Version"
			Write-Step "downloading $base/$zip"
			try { Get-HalotrcerFile "$base/$zip" (Join-Path $tmp $zip) } catch { throw "download failed: $base/$zip" }

			# Step 7: verify the signature, then the checksum.
			if (-not $NoVerify) {
				$sums = Join-Path $tmp 'checksums.txt'
				try { Get-HalotrcerFile "$base/checksums.txt" $sums } catch { throw "download failed: $base/checksums.txt" }

				# The signature first: it is what makes the checksum file trustworthy.
				if ($cosign) {
					$bundle = Join-Path $tmp 'checksums.txt.sigstore.json'
					try { Get-HalotrcerFile "$base/checksums.txt.sigstore.json" $bundle } catch { throw "download failed: $base/checksums.txt.sigstore.json" }
					# cosign reports success on stderr; the exit code decides.
					$v = Invoke-HalotrcerNative $cosign.Source @('verify-blob', '--bundle', $bundle,
						'--certificate-identity-regexp', $script:HalotrcerCosignIdentityRegexp,
						'--certificate-oidc-issuer', $script:HalotrcerCosignOidcIssuer, $sums)
					if ($v.ExitCode -ne 0) {
						throw "cosign could not verify checksums.txt as signed by $script:HalotrcerRepo's release workflow; nothing was installed"
					}
					Write-Step "cosign: checksums.txt is signed by $script:HalotrcerRepo's release workflow"
				} else {
					Write-Step 'cosign not found: the signature was not checked (SHA-256 only; install cosign or pass -RequireCosign)'
				}

				# Then the zip's SHA-256 against its exact line in checksums.txt.
				$want = $null
				foreach ($l in Get-Content -LiteralPath $sums) {
					$parts = $l -split '\s+', 2
					if ($parts.Count -eq 2 -and $parts[1] -eq $zip) { $want = $parts[0].ToLowerInvariant() }
				}
				if (-not $want) { throw "$zip is not listed in checksums.txt; nothing was installed" }
				$got = (Get-FileHash -LiteralPath (Join-Path $tmp $zip) -Algorithm SHA256).Hash.ToLowerInvariant()
				if ($got -ne $want) { throw "SHA-256 mismatch for $zip (expected $want, got $got); nothing was installed" }
				Write-Step 'SHA-256 verified'
			} else {
				Write-Warning 'halotrcer-install: -NoVerify: the download''s SHA-256 and signature were NOT checked'
			}

			# Step 8: unpack and check the binary runs.
			$unpacked = Join-Path $tmp 'unpacked'
			Expand-Archive -LiteralPath (Join-Path $tmp $zip) -DestinationPath $unpacked
			$exe = Join-Path $unpacked 'halotrcer.exe'
			if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) { throw "halotrcer.exe is not in $zip" }
			if (-not (Get-HalotrcerVersionLine $exe)) { throw 'the downloaded binary does not run on this machine' }

			# Step 9: install atomically (not with -Only). Copy next to the
			# destination, then rename over it, so an interrupted install never
			# leaves half a halotrcer.exe.
			if ($Only) {
				$hal = $exe
			} else {
				New-Item -ItemType Directory -Path $halDir -Force | Out-Null
				$staged = Join-Path $halDir '.halotrcer.new.exe'
				Copy-Item -LiteralPath $exe -Destination $staged -Force
				try {
					Move-Item -LiteralPath $staged -Destination $target -Force
				} catch {
					Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
					throw "could not replace $target (is halotrcer running?): $($_.Exception.Message)"
				}
				$hal = $target
				Write-Step "installed: $(Get-HalotrcerVersionLine $target)"
			}
		}

		# Step 10: Tokvera and Sievant, through halotrcer. A tool installed
		# elsewhere (Scoop, winget, go install) is left alone: its own channel
		# upgrades it, and `halotrcer doctor` checks its version. halotrcer
		# reads the same mirror variables and records what it installs, so
		# `halotrcer uninstall --all` can remove it later.
		$dirs = @()
		if (-not $Only) { $dirs += $halDir }
		$failed = @()
		foreach ($t in $tools) {
			$toolDir = Get-HalotrcerDir $t $Dir -Global:$Global
			$other = Find-HalotrcerTool $t
			if (-not (Test-Path -LiteralPath (Join-Path $toolDir "$t.exe") -PathType Leaf) -and $other) {
				Write-Step "$t is already installed at $other; leaving it as it is (halotrcer doctor checks its version)"
				continue
			}
			Write-Step "installing $t with halotrcer (its own release, verified against its own release workflow)"
			New-Item -ItemType Directory -Path $toolDir -Force | Out-Null
			$r = Invoke-HalotrcerToolInstall $hal (@('install', $t, '--via', 'direct', '--dir', $toolDir) + $verifyFlag) $toolDir
			# halotrcer's own "next:" line is replaced by this script's, below.
			foreach ($l in (($r.StdOut + $r.StdErr) -split "`r?`n")) {
				if ($l -and -not $l.StartsWith('next: ')) { Write-Host "  $l" }
			}
			if ($r.ExitCode -ne 0) {
				Write-Step "error: $t was not installed"
				$failed += $t
				continue
			}
			if ($dirs -notcontains $toolDir) { $dirs += $toolDir }
		}
		if ($failed.Count -gt 0) {
			throw "not everything was installed (see above): $($failed -join ', '); run the script again, or halotrcer install <tool>"
		}

		# Step 11: PATH hints (PATH itself is never edited here), and the next
		# command.
		foreach ($d in $dirs) {
			$onPath = @($env:Path -split ';' | Where-Object { $_.TrimEnd('\') -ieq $d.TrimEnd('\') }).Count -gt 0
			if (-not $onPath) {
				Write-Step "$d is not on your PATH; to add it for your user, run:"
				Write-Step "  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';$d', 'User')"
			}
		}
		switch ($Only) {
			'' { Write-Step 'next: halotrcer check --target <inference-url>' }
			'tokvera' { Write-Step 'next: tokvera scan --target <inference-url>' }
			'sievant' { Write-Step 'next: sievant scan' }
		}
	} finally {
		Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
	}
}

# --- Entry point ----------------------------------------------------------------------

# Maps the outcome to an exit code: 0 done, 1 failure, 2 usage error.
function Invoke-HalotrcerInstallMain([hashtable]$Options) {
	try {
		Install-Halotrcer @Options
		return 0
	} catch [HalotrcerUsageError] {
		Write-Host "halotrcer-install: error: $($_.Exception.Message)"
		Write-Host 'halotrcer-install: run Get-Help .\install.ps1 for usage'
		return 2
	} catch {
		Write-Host "halotrcer-install: error: $($_.Exception.Message)"
		return 1
	}
}

# Dot-sourcing (". .\install.ps1") only defines the functions above; the tests
# use that. Run as a file, the script exits with the code; run through iex or a
# scriptblock it must not call exit, which would close the user's shell, so it
# sets $LASTEXITCODE instead.
if ($MyInvocation.InvocationName -ne '.') {
	$script:HalotrcerExitCode = Invoke-HalotrcerInstallMain @{
		Only = $Only; Version = $Version; Dir = $Dir; Global = $Global; DryRun = $DryRun
		NoVerify = $NoVerify; RequireCosign = $RequireCosign
	}
	if ($PSCommandPath) {
		exit $script:HalotrcerExitCode
	}
	$global:LASTEXITCODE = $script:HalotrcerExitCode
}
