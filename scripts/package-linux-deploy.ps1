param([switch]$SkipWebBuild, [switch]$IncludeWorkerImage)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression.FileSystem
Add-Type -AssemblyName System.IO.Compression
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$releaseVersion = Get-Date -Format 'yyyyMMdd-HHmmss'
$distDir = Join-Path $projectRoot 'dist'
$packageDir = Join-Path $distDir "releases/$releaseVersion/GoStream3D-linux-deploy-amd64"
Push-Location -LiteralPath $projectRoot
try {
    if (-not $SkipWebBuild) {
        Push-Location -LiteralPath (Join-Path $projectRoot 'web')
        try {
            npm ci
            if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
            npm run build
            if ($LASTEXITCODE -ne 0) { throw 'web build failed' }
        } finally { Pop-Location }
        Copy-Item -LiteralPath web/dist/index.html -Destination internal/api/static/index.html -Force
        Get-ChildItem -LiteralPath web/dist/assets -File | Copy-Item -Destination internal/api/static/assets -Force
    }
    # Scope to gateway packages; ./... also walks archived browser profiles in dist.
    go test ./cmd/... ./internal/...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    New-Item -ItemType Directory -Path "$packageDir/runtime-sources/runtime" -Force | Out-Null
    $previousGOOS = $env:GOOS
    $previousGOARCH = $env:GOARCH
    $previousCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
        go build -trimpath -ldflags "-s -w -X main.version=$releaseVersion" -o "$packageDir/gostream3d" ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw 'Linux build failed' }
    } finally {
        $env:GOOS = $previousGOOS; $env:GOARCH = $previousGOARCH; $env:CGO_ENABLED = $previousCGO
    }
    # Include the modified sources, not development credentials or project data.
    if (-not (Test-Path -LiteralPath neko-3.1.5/server/vendor/modules.txt)) {
        throw 'Neko vendor cache missing. Run go mod vendor in neko-3.1.5/server first.'
    }
    Copy-Item -LiteralPath neko-3.1.5/server -Destination "$packageDir/runtime-sources/server" -Recurse
    Copy-Item -LiteralPath neko-3.1.5/utils/xorg-deps -Destination "$packageDir/runtime-sources/xorg-deps" -Recurse
    foreach ($runtimeFile in @('xorg.conf', 'default.pa', 'supervisord.conf', 'supervisord.dbus.conf', 'dbus')) {
        Copy-Item -LiteralPath "neko-3.1.5/runtime/$runtimeFile" -Destination "$packageDir/runtime-sources/runtime/$runtimeFile"
    }
    Copy-Item -LiteralPath neko-3.1.5/apps/chromium/launch-gostream3d.sh -Destination "$packageDir/runtime-sources/launch-gostream3d.sh"
    Get-ChildItem -LiteralPath neko-3.1.5 -Filter 'LICENSE*' -File | Copy-Item -Destination "$packageDir/runtime-sources"
    New-Item -ItemType Directory -Path "$packageDir/deploy" -Force | Out-Null
    Copy-Item -LiteralPath deploy/Dockerfile.worker, deploy/supervisord.worker.conf, deploy/sync-chromium-window.sh -Destination "$packageDir/deploy"
    Copy-Item -LiteralPath deploy/dockerignore.worker -Destination "$packageDir/.dockerignore"
    foreach ($script in @('install-linux.sh', 'start-linux.sh')) {
        # ZIP must carry LF shell scripts even when this project has CRLF files.
        $scriptText = [IO.File]::ReadAllText((Join-Path $PSScriptRoot $script)).Replace("`r`n", "`n")
        [IO.File]::WriteAllText((Join-Path $packageDir $script), $scriptText, [Text.UTF8Encoding]::new($false))
    }
    Copy-Item -LiteralPath docs/linux-quick-deploy.md -Destination "$packageDir/README.md"
    Copy-Item -LiteralPath docs/linux-deploy-verification.md -Destination "$packageDir/VERIFICATION.md"
    Copy-Item -LiteralPath config.linux.example.env -Destination "$packageDir/config.example.env"
    if ($IncludeWorkerImage) {
        docker save -o "$packageDir/worker.tar" gostream3d/neko/chromium:3.1.5
        if ($LASTEXITCODE -ne 0) { throw 'Worker image export failed' }
    }
    $zipPath = Join-Path $distDir 'GoStream3D-linux-deploy-amd64.zip'
    if (Test-Path -LiteralPath $zipPath) {
        Move-Item -LiteralPath $zipPath -Destination "$zipPath.$releaseVersion.bak"
    }
    # Windows PowerShell/.NET Framework can otherwise write backslash ZIP names.
    $linuxZipArchive = [IO.Compression.ZipFile]::Open($zipPath, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($packageFile in Get-ChildItem -LiteralPath $packageDir -Recurse -File) {
            $entryName = $packageFile.FullName.Substring($packageDir.Length + 1).Replace('\', '/')
            [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($linuxZipArchive, $packageFile.FullName, $entryName, [IO.Compression.CompressionLevel]::Optimal) | Out-Null
        }
    } finally { $linuxZipArchive.Dispose() }
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $zipPath).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText((Join-Path $distDir 'GoStream3D-linux-deploy-amd64.sha256'), "$hash  GoStream3D-linux-deploy-amd64.zip`n", [Text.UTF8Encoding]::new($false))
    Write-Host "Built $zipPath"
} finally { Pop-Location }
