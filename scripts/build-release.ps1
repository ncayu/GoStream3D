$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Push-Location -LiteralPath $projectRoot
try {
    Push-Location -LiteralPath (Join-Path $projectRoot 'web')
    try {
        npm ci
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
        npm run build
        if ($LASTEXITCODE -ne 0) { throw 'web build failed' }
    } finally { Pop-Location }

    $assetDir = Join-Path $projectRoot 'internal/api/static/assets'
    New-Item -ItemType Directory -Force -Path $assetDir | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'dist') | Out-Null
    Get-ChildItem -LiteralPath $assetDir -Filter 'index-*' -File | Remove-Item
    Copy-Item -LiteralPath (Join-Path $projectRoot 'web/dist/index.html') -Destination (Join-Path $projectRoot 'internal/api/static/index.html') -Force
    Get-ChildItem -LiteralPath (Join-Path $projectRoot 'web/dist/assets') -File | Copy-Item -Destination $assetDir -Force

    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    $oldCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        go build -trimpath -o (Join-Path $projectRoot 'dist/gostream3d-linux-amd64') ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    } finally {
        $env:GOOS = $oldGOOS
        $env:GOARCH = $oldGOARCH
        $env:CGO_ENABLED = $oldCGO
    }
    Write-Host 'Built dist/gostream3d-linux-amd64 with embedded web UI'
} finally { Pop-Location }
