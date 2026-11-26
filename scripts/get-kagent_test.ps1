# Tests scripts/get-kagent.ps1 against a fake GitHub: a local HTTP listener
# answers the /releases/latest redirect and serves binaries from a fixture
# directory. Requires Windows and Go, which builds the fake kagent binaries.
Set-StrictMode -Version 2
$ErrorActionPreference = 'Stop'

$installer = Join-Path $PSScriptRoot 'get-kagent.ps1'
$shell = (Get-Process -Id $PID).Path
$work = Join-Path ([IO.Path]::GetTempPath()) "get-kagent-test-$([Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $work | Out-Null

# The fake binary answers only `version --client`, without the tag's leading v,
# as release builds do. Anything else records a server call.
Set-Content -Encoding ascii -LiteralPath (Join-Path $work 'main.go') -Value @'
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var version string

func main() {
	if len(os.Args) == 3 && os.Args[1] == "version" && os.Args[2] == "--client" {
		fmt.Printf("{\"build_date\":\"\",\"git_commit\":\"\",\"kagent_version\":\"%s\"}\n", version)
		return
	}
	exe, _ := os.Executable()
	_ = os.WriteFile(filepath.Join(filepath.Dir(exe), "server-contacted"), nil, 0o600)
	os.Exit(1)
}
'@
Set-Content -Encoding ascii -LiteralPath (Join-Path $work 'go.mod') -Value "module fakekagent`n`ngo 1.21"

# Build-FakeKagent TAG builds the binary once per tag and returns its path.
function Build-FakeKagent([string]$Tag) {
    $binary = Join-Path $work "fake-$Tag.exe"
    if (-not (Test-Path -LiteralPath $binary)) {
        Push-Location $work
        try {
            $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
            & go build -ldflags "-X main.version=$($Tag.TrimStart('v'))" -o "fake-$Tag.exe" .
            if ($LASTEXITCODE -ne 0) { throw "go build of the fake kagent failed" }
        } finally {
            Pop-Location
        }
    }
    $binary
}

$listener = $null
foreach ($attempt in 1..20) {
    $port = Get-Random -Minimum 20000 -Maximum 60000
    $candidate = New-Object Net.HttpListener
    $candidate.Prefixes.Add("http://127.0.0.1:$port/")
    try { $candidate.Start(); $listener = $candidate; break } catch { }
}
if (-not $listener) { throw 'could not start the fake GitHub listener' }
$origin = "http://127.0.0.1:$port"

# The server redirects /<fixture>/releases/latest to the tag in <fixture>\latest,
# serves <fixture>\download\..., and logs each requested path.
$server = [PowerShell]::Create()
[void]$server.AddScript({
    param($listener, $work, $origin)
    while ($listener.IsListening) {
        try { $context = $listener.GetContext() } catch { break }
        $response = $context.Response
        try {
            $parts = $context.Request.Url.AbsolutePath.TrimStart('/') -split '/', 3
            $fixture = Join-Path $work $parts[0]
            $path = $parts[2]
            Add-Content -LiteralPath (Join-Path $fixture 'requests') -Value $path
            $latest = Join-Path $fixture 'latest'
            $file = Join-Path $fixture $path
            if ($path -eq 'latest' -and (Test-Path -LiteralPath $latest)) {
                $response.StatusCode = 302
                $response.RedirectLocation = "$origin/$($parts[0])/releases/tag/$((Get-Content -LiteralPath $latest).Trim())"
            } elseif ($path -like 'download/*' -and (Test-Path -LiteralPath $file -PathType Leaf)) {
                $bytes = [IO.File]::ReadAllBytes($file)
                $response.ContentLength64 = $bytes.Length
                if ($context.Request.HttpMethod -ne 'HEAD') { $response.OutputStream.Write($bytes, 0, $bytes.Length) }
            } else {
                $response.StatusCode = 404
            }
        } catch {
            $response.StatusCode = 500
        } finally {
            $response.Close()
        }
    }
}).AddArgument($listener).AddArgument($work).AddArgument($origin)
$serving = $server.BeginInvoke()

# New-Fixture starts a scenario with an empty fake GitHub and install directory.
function New-Fixture([string]$Name) {
    $script:fixtureName = $Name
    $script:fixture = Join-Path $work $Name
    $script:installDir = Join-Path $fixture 'install'
    New-Item -ItemType Directory -Force -Path (Join-Path $fixture 'download'), $installDir | Out-Null
    Set-Content -LiteralPath (Join-Path $fixture 'requests') -Value $null
}

# Publish TAG creates the downloadable Windows binary and its checksum, in the
# `<hash>  <build path>` form the release publishes.
function Publish([string]$Tag, [string]$Checksum) {
    $directory = Join-Path $fixture "download\$Tag"
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
    $asset = Join-Path $directory 'kagent-windows-amd64.exe'
    Copy-Item -LiteralPath (Build-FakeKagent $Tag) -Destination $asset
    if (-not $Checksum) { $Checksum = (Get-FileHash -Algorithm SHA256 -LiteralPath $asset).Hash.ToLower() }
    Set-Content -Encoding ascii -LiteralPath "$asset.sha256" -Value "$Checksum  core/bin/kagent-windows-amd64.exe"
}

# Invoke-Installer ARGS... runs get-kagent.ps1 in a fresh shell and saves its output.
function Invoke-Installer {
    $env:KAGENT_RELEASES = "$origin/$fixtureName/releases"
    $env:KAGENT_INSTALL_DIR = $installDir
    $env:KAGENT_VERSION = $null
    # Under 'Stop', Windows PowerShell turns the installer's captured stderr into a terminating error.
    $ErrorActionPreference = 'Continue'
    $script:output = (& $shell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $installer -NoPathUpdate @args 2>&1 | Out-String)
    $script:status = $LASTEXITCODE
}

function Fail([string]$Message) {
    throw "FAIL: ${scenario}: $Message`n--- installer output ---`n$output"
}

function Assert-Installed([string]$Want) {
    if ($status -ne 0) { Fail "installer exited $status" }
    $binary = Join-Path $installDir 'kagent.exe'
    if (-not (Test-Path -LiteralPath $binary)) { Fail 'kagent.exe was not installed' }
    $got = & $binary version --client
    if ($got -notlike "*`"kagent_version`":`"$Want`"*") { Fail "installed $got, want $Want" }
}

function Get-Requests { @(Get-Content -LiteralPath (Join-Path $fixture 'requests') | Where-Object { $_ }) }

try {
    $scenario = 'Given GitHub marks a release latest, when installing without -Version, assert that the latest release is installed'
    New-Fixture latest
    Set-Content -LiteralPath (Join-Path $fixture 'latest') -Value 'v1.0.0'
    Publish v1.0.0
    Publish v0.10.7
    Invoke-Installer
    Assert-Installed 1.0.0

    $scenario = 'Given a requested version without a leading v, when installing with -Version, assert that it is installed without resolving latest'
    New-Fixture pinned
    Set-Content -LiteralPath (Join-Path $fixture 'latest') -Value 'v1.0.0'
    Publish v0.10.7
    Invoke-Installer -Version 0.10.7
    Assert-Installed 0.10.7
    if ((Get-Requests) -contains 'latest') { Fail 'resolved latest despite -Version' }

    $scenario = 'Given latest cannot be resolved, when installing without -Version, assert that the installer fails and suggests -Version'
    New-Fixture no-latest
    Invoke-Installer
    if ($status -eq 0) { Fail 'installer succeeded' }
    if ($output -notmatch '-Version') { Fail 'output does not suggest -Version' }

    $scenario = 'Given the release lacks a Windows binary, when installing, assert that the installer fails without installing'
    New-Fixture missing-asset
    Set-Content -LiteralPath (Join-Path $fixture 'latest') -Value 'v1.0.0'
    Invoke-Installer
    if ($status -eq 0) { Fail 'installer succeeded' }
    if (Test-Path -LiteralPath (Join-Path $installDir 'kagent.exe')) { Fail 'installed a binary' }

    $scenario = 'Given a binary that does not match its published checksum, when installing, assert that the installer fails without installing'
    New-Fixture bad-checksum
    Set-Content -LiteralPath (Join-Path $fixture 'latest') -Value 'v1.0.0'
    Publish v1.0.0 ('0' * 64)
    Invoke-Installer
    if ($status -eq 0) { Fail 'installer succeeded' }
    if ($output -notmatch 'SHA256') { Fail 'output does not report the checksum mismatch' }
    if (Test-Path -LiteralPath (Join-Path $installDir 'kagent.exe')) { Fail 'installed a binary' }

    $scenario = 'Given an older version is installed, when installing, assert that it is replaced by latest'
    New-Fixture upgrade
    Set-Content -LiteralPath (Join-Path $fixture 'latest') -Value 'v1.0.0'
    Publish v1.0.0
    Copy-Item -LiteralPath (Build-FakeKagent v0.10.7) -Destination (Join-Path $installDir 'kagent.exe')
    Invoke-Installer
    Assert-Installed 1.0.0

    $scenario = 'Given latest is installed, when installing again, assert that nothing is downloaded and no server is contacted'
    New-Fixture already-installed
    Set-Content -LiteralPath (Join-Path $fixture 'latest') -Value 'v1.0.0'
    Copy-Item -LiteralPath (Build-FakeKagent v1.0.0) -Destination (Join-Path $installDir 'kagent.exe')
    Invoke-Installer
    if ($status -ne 0) { Fail "installer exited $status" }
    if ($output -notmatch 'kagent v1\.0\.0 is already latest') { Fail 'did not detect the installed version' }
    if (Get-Requests | Where-Object { $_ -like 'download/*' }) { Fail 'downloaded despite a matching install' }
    if (Test-Path -LiteralPath (Join-Path $installDir 'server-contacted')) { Fail 'version check contacted the server' }

    Write-Host 'get-kagent.ps1 tests passed'
} finally {
    $listener.Stop()
    $server.Dispose()
    Remove-Item -Recurse -Force -LiteralPath $work -ErrorAction SilentlyContinue
}
