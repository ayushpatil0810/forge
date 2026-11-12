# PowerShell helper to run Forge autonomous fix loop against a test fixture.
param(
    [Parameter(Position=0)]
    [ValidateSet("offbyone", "nilpanic", "wrongimport", "boundary")]
    [string]$Fixture = "boundary",

    [switch]$Interactive,
    [switch]$Yolo
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
$fixtureDir = Join-Path $repoRoot "testdata\fixfail\$Fixture"

if (-not (Test-Path $fixtureDir)) {
    Write-Error "Fixture directory not found: $fixtureDir"
    exit 1
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "forge-fixture-$Fixture-$([System.Guid]::NewGuid().ToString('N').Substring(0, 8))"
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
Write-Host "==> Staging fixture '$Fixture' into $tempDir" -ForegroundColor Cyan
Copy-Item -Path "$fixtureDir\*" -Destination $tempDir -Recurse -Force

# Show initial failure
Push-Location $tempDir
try {
    Write-Host "`n==> Verifying initial test failure in $tempDir:" -ForegroundColor Yellow
    go test ./... 2>&1 | Write-Host
    Write-Host "(Failure verified as expected)`n" -ForegroundColor DarkGray

    # Locate or build forge binary
    $forgeBin = Join-Path $repoRoot "forge.exe"
    if (-not (Test-Path $forgeBin)) {
        Write-Host "==> Building forge binary..." -ForegroundColor Cyan
        go build -o $forgeBin (Join-Path $repoRoot "cmd\forge")
    }

    $prompt = "Tests are failing in this project. Use bash to run the tests, read the relevant files, fix the root cause with edit_file, and re-run the tests to confirm they pass."

    if ($Interactive) {
        Write-Host "==> Launching Forge interactive TUI..." -ForegroundColor Green
        & $forgeBin --root $tempDir
    } else {
        $argsList = @("--root", $tempDir, "--yes")
        if ($Yolo) {
            $argsList += "--yolo"
        }
        $argsList += $prompt

        Write-Host "==> Running Forge headless fix loop..." -ForegroundColor Green
        & $forgeBin @argsList

        Write-Host "`n==> Verifying post-fix test status:" -ForegroundColor Yellow
        go test -v ./...
        Write-Host "==> Fixture '$Fixture' fixed successfully!" -ForegroundColor Green
    }
}
finally {
    Pop-Location
    Write-Host "`nWorkspace retained at: $tempDir" -ForegroundColor DarkGray
}
