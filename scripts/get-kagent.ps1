# Copyright The kagent Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

<#
.SYNOPSIS
Installs the kagent CLI on Windows. The PowerShell counterpart of get-kagent.

.DESCRIPTION
Downloads kagent-windows-amd64.exe from a GitHub release, verifies its SHA256
checksum, installs it as kagent.exe and adds the install directory to the user
PATH. No administrator rights are needed.

  irm https://raw.githubusercontent.com/kagent-dev/kagent/main/scripts/get-kagent.ps1 | iex

Piping to iex cannot pass parameters, so each one also reads an environment
variable. To pass parameters, run it as a script block:

  & ([scriptblock]::Create((irm <url>))) -Version v1.0.0

.PARAMETER Version
Release to install, such as v1.0.0. Defaults to KAGENT_VERSION, or else the
release GitHub marks latest.

.PARAMETER InstallDir
Directory to install kagent.exe into. Defaults to KAGENT_INSTALL_DIR, or else
%LOCALAPPDATA%\Programs\kagent.

.PARAMETER NoPathUpdate
Leave the user PATH unchanged.

.PARAMETER NoVerifyChecksum
Skip the SHA256 check. VERIFY_CHECKSUM=false does the same.
#>
param(
    [string]$Version = $env:KAGENT_VERSION,
    [string]$InstallDir = $env:KAGENT_INSTALL_DIR,
    [switch]$NoPathUpdate,
    [switch]$NoVerifyChecksum
)

# Everything runs inside one function so that `irm | iex`, which evaluates this
# file in the caller's scope, leaves no preferences or variables behind. Errors
# are thrown rather than passed to `exit`, which would close the caller's shell.
function Install-Kagent {
    param([string]$Version, [string]$InstallDir, [bool]$UpdatePath, [bool]$VerifyChecksum)

    Set-StrictMode -Version 2
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell draws a progress bar that slows downloads many times over.
    $ProgressPreference = 'SilentlyContinue'
    # Windows PowerShell may default to TLS versions that GitHub refuses.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    # KAGENT_RELEASES points the installer at a fake GitHub in tests.
    $releases = if ($env:KAGENT_RELEASES) { $env:KAGENT_RELEASES.TrimEnd('/') } else { 'https://github.com/kagent-dev/kagent/releases' }

    # A 32-bit PowerShell on 64-bit Windows reports the real one in PROCESSOR_ARCHITEW6432.
    $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    # Keep in sync with the release binaries built by go/Makefile. Windows on
    # ARM64 runs the amd64 binary under emulation.
    if ($arch -notin 'AMD64', 'ARM64') {
        throw "No prebuilt kagent binary for windows-$arch. To build from source, go to https://github.com/kagent-dev/kagent"
    }
    $asset = 'kagent-windows-amd64.exe'

    if ($Version) {
        if (-not $Version.StartsWith('v')) { $Version = "v$Version" }
        $tag = $Version
    } else {
        # The redirect of /releases/latest names the tag. Unlike the GitHub
        # API, it is not rate limited.
        $location = $null
        try {
            $request = [Net.HttpWebRequest]::Create("$releases/latest")
            $request.Method = 'HEAD'
            $request.AllowAutoRedirect = $false
            $response = $request.GetResponse()
            $location = $response.Headers['Location']
            $response.Close()
        } catch [Net.WebException] {
            # Unreachable, or an error status with no redirect: reported below.
        }
        if ($location -notmatch '/releases/tag/([^/?#]+)') {
            throw "Could not resolve the latest kagent release from $releases/latest. Set KAGENT_VERSION or pass -Version to choose a release."
        }
        $tag = $Matches[1]
    }

    if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\kagent' }
    $target = Join-Path $InstallDir 'kagent.exe'

    # Ask only the CLI for its version, never a kagent server.
    $installed = $null
    if (Test-Path -LiteralPath $target) {
        try {
            # Under 'Stop', Windows PowerShell turns redirected stderr into a terminating error.
            $ErrorActionPreference = 'Continue'
            $output = (& $target version --client 2>$null) -join "`n"
            $ErrorActionPreference = 'Stop'
            if ($output -match '"kagent_version":\s*"([^"]*)"') { $installed = $Matches[1] }
        } catch {
            # A binary that cannot report its version is replaced.
            $ErrorActionPreference = 'Stop'
        }
    }

    # The CLI reports 1.0.0 for the release tagged v1.0.0.
    if ($installed -and $installed.TrimStart('v') -eq $tag.TrimStart('v')) {
        $wanted = if ($Version) { $Version } else { 'latest' }
        Write-Host "kagent $tag is already $wanted"
    } else {
        if (Test-Path -LiteralPath $target) {
            $from = if ($installed) { $installed } else { 'unknown' }
            Write-Host "kagent $tag is available. Changing from version $from."
        }
        $url = "$releases/download/$tag/$asset"
        $temp = Join-Path ([IO.Path]::GetTempPath()) "kagent-installer-$([Guid]::NewGuid().ToString('N'))"
        New-Item -ItemType Directory -Path $temp | Out-Null
        try {
            $download = Join-Path $temp $asset
            Write-Host "Downloading $url"
            try {
                if ($VerifyChecksum) {
                    Invoke-WebRequest -UseBasicParsing -Uri "$url.sha256" -OutFile "$download.sha256"
                }
                Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $download
            } catch {
                throw "Failed to download $url. Check that release $tag exists and publishes a Windows binary. $($_.Exception.Message)"
            }
            if ($VerifyChecksum) {
                $expected = (-split (Get-Content -Raw -LiteralPath "$download.sha256"))[0]
                $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $download).Hash
                if ($actual -ne $expected) {
                    throw "SHA256 of $asset is $actual, but the release publishes $expected. Aborting."
                }
                Write-Host 'Verified checksum.'
            }
            New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
            try {
                Copy-Item -LiteralPath $download -Destination $target -Force
            } catch {
                throw "Could not write $target. Stop any running kagent commands and try again. $($_.Exception.Message)"
            }
            Write-Host "kagent $tag installed into $target"
        } finally {
            Remove-Item -Recurse -Force -LiteralPath $temp -ErrorAction SilentlyContinue
        }
    }

    $directory = $InstallDir.TrimEnd('\')
    $onPath = { param($path) @("$path" -split ';' | Where-Object { $_.TrimEnd('\') -eq $directory }).Count -gt 0 }
    if ($UpdatePath) {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not (& $onPath $userPath)) {
            [Environment]::SetEnvironmentVariable('Path', (@($userPath, $directory) | Where-Object { $_ }) -join ';', 'User')
            Write-Host "Added $directory to your user PATH. New terminals pick it up."
        }
        # Make kagent usable in this session too.
        if (-not (& $onPath $env:Path)) { $env:Path = "$env:Path;$directory" }
    } elseif (-not (& $onPath $env:Path)) {
        Write-Warning "$directory is not on your PATH. Add it to run kagent."
    }

    foreach ($tool in 'kubectl', 'helm') {
        if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
            Write-Warning "Could not find $tool. 'kagent install' requires it on your PATH."
        }
    }
}

Install-Kagent -Version $Version -InstallDir $InstallDir -UpdatePath (-not $NoPathUpdate) `
    -VerifyChecksum (-not ($NoVerifyChecksum -or $env:VERIFY_CHECKSUM -eq 'false'))
