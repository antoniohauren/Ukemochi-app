# Windows counterpart of the Makefile, for machines without make.
# Usage: .\make.ps1 gen | run | test | seed | build
param([Parameter(Position = 0)][ValidateSet('gen', 'run', 'test', 'seed', 'build')][string]$Target = 'run')

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$TailwindVersion = 'v4.1.13'
$TailwindAsset = 'tailwindcss-windows-x64.exe'
$Tailwind = 'bin\tailwindcss.exe'
$HtmxVersion = '2.0.4'
$Htmx = 'static\htmx.min.js'
$HtmxSha256 = 'e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447'

function Get-Checked($Url, $Out, $Sha256) {
    $tmp = "$Out.tmp"
    Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $tmp
    if ((Get-FileHash -Algorithm SHA256 $tmp).Hash -ne $Sha256.ToUpper()) {
        Remove-Item $tmp
        throw "checksum mismatch for $Url"
    }
    Move-Item -Force $tmp $Out
}

function Invoke-Native {
    & $args[0] $args[1..($args.Count - 1)]
    if ($LASTEXITCODE -ne 0) { throw "$($args -join ' ') failed with exit code $LASTEXITCODE" }
}

# Exports the variables in .env, if present, like the Makefile's DOTENV.
function Import-Dotenv {
    if (-not (Test-Path .env)) { return }
    foreach ($line in Get-Content .env) {
        if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') {
            Set-Item "env:$($Matches[1])" $Matches[2].Trim()
        }
    }
}

function Invoke-Gen {
    New-Item -ItemType Directory -Force bin | Out-Null
    if (-not (Test-Path $Tailwind)) {
        $base = "https://github.com/tailwindlabs/tailwindcss/releases/download/$TailwindVersion"
        # Saved to a file: Windows PowerShell returns .Content as bytes for this file's content type.
        Invoke-WebRequest -UseBasicParsing -Uri "$base/sha256sums.txt" -OutFile bin\sha256sums.txt
        $line = Get-Content bin\sha256sums.txt | Where-Object { $_ -match " (\./)?$([regex]::Escape($TailwindAsset))\s*$" }
        if (-not $line) { throw "no checksum for $TailwindAsset" }
        Get-Checked "$base/$TailwindAsset" $Tailwind ($line -split '\s+')[0]
    }
    if (-not (Test-Path $Htmx)) {
        Get-Checked "https://unpkg.com/htmx.org@$HtmxVersion/dist/htmx.min.js" $Htmx $HtmxSha256
    }
    Invoke-Native go tool templ generate
    Invoke-Native $Tailwind -i web/input.css -o static/app.css --minify
}

switch ($Target) {
    'gen' { Invoke-Gen }
    'run' { Invoke-Gen; Import-Dotenv; Invoke-Native go run ./cmd/ukemochi }
    'test' { Invoke-Gen; Invoke-Native go test ./... }
    'seed' { Import-Dotenv; Invoke-Native go run ./cmd/seed }
    'build' {
        Invoke-Gen
        $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
        Invoke-Native go build -ldflags=-H=windowsgui -o bin/ukemochi.exe ./cmd/ukemochi
    }
}
