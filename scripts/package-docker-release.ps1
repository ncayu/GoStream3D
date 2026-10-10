param([switch]$MirrorECR, [switch]$SkipWebBuild, [switch]$ExportOnly, [switch]$Validate, [ValidatePattern('^\d{8}-\d{6}$')][string]$Version)
$ErrorActionPreference = 'Stop'
$dockerReleaseBin = Split-Path -Parent (Get-Command docker).Source
if (Test-Path -LiteralPath (Join-Path $dockerReleaseBin 'docker-credential-desktop.exe')) {
    $env:PATH = "$dockerReleaseBin;" + $env:PATH
}
Add-Type -AssemblyName System.IO.Compression.FileSystem
Add-Type -AssemblyName System.IO.Compression
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$releaseStamp = [TimeZoneInfo]::ConvertTimeBySystemTimeZoneId([DateTimeOffset]::UtcNow, 'China Standard Time').ToString('yyyyMMdd-HHmmss')
if ($Version) { $releaseStamp = $Version }
$releaseRoot = Join-Path $projectRoot "dist/docker-releases/$releaseStamp"
if (Test-Path -LiteralPath $releaseRoot) { throw "Release timestamp $releaseStamp already exists; retry in a second to preserve it." }
$packageName = 'GoStream3D-docker-compose-linux-amd64'
$packageRoot = Join-Path $releaseRoot $packageName
function Invoke-DockerBuild([string[]]$DockerArgs) {
    & docker @DockerArgs
    if ($LASTEXITCODE -ne 0) { throw 'Docker command failed' }
}
Push-Location -LiteralPath $projectRoot
try {
    if (-not $ExportOnly) {
        if (-not $SkipWebBuild) {
            Push-Location -LiteralPath web
            try {
                npm ci
                if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
                npm test
                if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed' }
                npm run build
                if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
            } finally { Pop-Location }
        }
        go test ./cmd/... ./internal/...
        if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
        Push-Location -LiteralPath neko-3.1.5/server
        try {
            go test ./pkg/quality
            if ($LASTEXITCODE -ne 0) { throw 'Worker adaptive quality tests failed' }
        } finally { Pop-Location }
        $sourceRoot = Join-Path $releaseRoot 'source'
        $gatewayRoot = Join-Path $releaseRoot 'gateway'
        New-Item -ItemType Directory -Path "$sourceRoot/internal", "$gatewayRoot/web" -Force | Out-Null
        Copy-Item -LiteralPath go.mod, go.sum, cmd -Destination $sourceRoot -Recurse
        $internalRoot = (Resolve-Path -LiteralPath internal).Path
        foreach ($sourceFile in Get-ChildItem -LiteralPath internal -Recurse -File) {
            $relative = $sourceFile.FullName.Substring($internalRoot.Length + 1)
            if ($relative.StartsWith('api\static\')) { continue }
            $destination = Join-Path "$sourceRoot/internal" $relative
            New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
            Copy-Item -LiteralPath $sourceFile.FullName -Destination $destination
        }
        Copy-Item -LiteralPath web/dist -Destination "$sourceRoot/internal/api/static" -Recurse
        Copy-Item -LiteralPath web/dist -Destination "$gatewayRoot/web/dist" -Recurse
        Copy-Item -LiteralPath deploy/Dockerfile.gateway.release -Destination "$gatewayRoot/Dockerfile"
        Copy-Item -LiteralPath deploy/entrypoint.gateway.sh -Destination "$gatewayRoot/entrypoint.gateway.sh"
        $previousGOOS = $env:GOOS; $previousGOARCH = $env:GOARCH; $previousCGO = $env:CGO_ENABLED
        Push-Location -LiteralPath $sourceRoot
        try {
            $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
            go build -trimpath -ldflags "-s -w -X main.version=$releaseStamp" -o "$gatewayRoot/gostream3d" ./cmd/server
            if ($LASTEXITCODE -ne 0) { throw 'Linux gateway build failed' }
        } finally {
            $env:GOOS = $previousGOOS; $env:GOARCH = $previousGOARCH; $env:CGO_ENABLED = $previousCGO
            Pop-Location
        }
        $baseArgs = @()
        if ($MirrorECR) { $baseArgs = @('--build-arg', 'RUNTIME_IMAGE=public.ecr.aws/docker/library/debian:trixie-slim') }
        Invoke-DockerBuild (@('build') + $baseArgs + @('--build-arg', "VERSION=$releaseStamp", '-t', "gostream3d/gateway:$releaseStamp", '-t', 'gostream3d/gateway:latest', $gatewayRoot))
        $workerRoot = Join-Path $releaseRoot 'worker'
        New-Item -ItemType Directory -Path "$workerRoot/runtime-sources/runtime", "$workerRoot/deploy" -Force | Out-Null
        Copy-Item -LiteralPath neko-3.1.5/server -Destination "$workerRoot/runtime-sources/server" -Recurse
        Copy-Item -LiteralPath neko-3.1.5/utils/xorg-deps -Destination "$workerRoot/runtime-sources/xorg-deps" -Recurse
        foreach ($runtimeFile in @('xorg.conf', 'default.pa', 'supervisord.conf', 'supervisord.dbus.conf', 'dbus')) {
            Copy-Item -LiteralPath "neko-3.1.5/runtime/$runtimeFile" -Destination "$workerRoot/runtime-sources/runtime/$runtimeFile"
        }
        Copy-Item -LiteralPath neko-3.1.5/apps/chromium/launch-gostream3d.sh -Destination "$workerRoot/runtime-sources/launch-gostream3d.sh"
        Copy-Item -LiteralPath deploy/Dockerfile.worker, deploy/supervisord.worker.conf, deploy/sync-chromium-window.sh, deploy/entrypoint.worker.sh -Destination "$workerRoot/deploy"
        Copy-Item -LiteralPath deploy/dockerignore.worker -Destination "$workerRoot/.dockerignore"
        if ($MirrorECR) { $baseArgs += @('--build-arg', 'GO_IMAGE=public.ecr.aws/docker/library/golang:1.25-trixie') }
        Invoke-DockerBuild (@('build') + $baseArgs + @('--build-arg', "VERSION=$releaseStamp", '-f', "$workerRoot/deploy/Dockerfile.worker", '-t', 'gostream3d/neko/chromium:3.1.5', '-t', "gostream3d/neko/chromium:3.1.5-$releaseStamp", '-t', 'gostream3d/neko/nvidia-chromium:3.1.5', '-t', "gostream3d/neko/nvidia-chromium:3.1.5-$releaseStamp", $workerRoot))
    }
    if ($Validate) { & (Join-Path $PSScriptRoot 'test-docker-image.ps1') -StandaloneCompose }
    New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null
    $versionText = (& docker run --rm gostream3d/gateway:latest --version) -join ''
    if ($LASTEXITCODE -ne 0 -or $versionText -notmatch '^GoStream3D (\d{8}-\d{6})$') { throw 'Gateway release version is missing' }
    $builtVersion = $Matches[1]
    $imageNames = @('gostream3d/gateway:latest', "gostream3d/gateway:$builtVersion", 'gostream3d/neko/chromium:3.1.5', "gostream3d/neko/chromium:3.1.5-$builtVersion", 'gostream3d/neko/nvidia-chromium:3.1.5', "gostream3d/neko/nvidia-chromium:3.1.5-$builtVersion")
    $images = @(& docker image inspect @imageNames | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0) { throw 'Release images are missing' }
    foreach ($image in $images) {
        if ($image.Os -ne 'linux' -or $image.Architecture -ne 'amd64') { throw 'Release requires linux/amd64 images' }
    }
    Invoke-DockerBuild (@('save', '-o', "$packageRoot/images.tar") + $imageNames)
    $imageHash = (Get-FileHash -LiteralPath "$packageRoot/images.tar" -Algorithm SHA256).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText("$packageRoot/images.tar.sha256", "$imageHash  images.tar`n", [Text.UTF8Encoding]::new($false))
    foreach ($file in @('docker-compose.yml', 'deploy/docker.env.example', 'scripts/start-docker.sh', 'docs/docker-linux-deploy.md', 'docs/docker-hub-deploy.md', 'docs/docker-hub/gateway-overview.md', 'docs/docker-hub/worker-overview.md', 'docs/admin-account.md', 'docs/linux-gpu-rendering.md', 'docs/linux-nvidia-wsl-rendering.md', 'docs/linux-gpu-interaction.md', 'docs/linux-resource-control.md', 'docs/linux-gpu-quality.md', 'docs/linux-stream-performance.md', 'docs/linux-gpu-depth-stability.md')) {
        $name = Split-Path -Leaf $file
        if ($name -eq 'docker-linux-deploy.md') { $name = 'README.md' }
        $content = [IO.File]::ReadAllText((Join-Path $projectRoot $file)).Replace("`r`n", "`n")
        [IO.File]::WriteAllText((Join-Path $packageRoot $name), $content, [Text.UTF8Encoding]::new($false))
        if ($name -eq 'start-docker.sh') {
            [IO.File]::WriteAllText((Join-Path $packageRoot 'start.sh'), $content, [Text.UTF8Encoding]::new($false))
        }
    }
    $runtimeValidation = @{ status = 'not recorded for these images'; command = 'powershell -File scripts/test-docker-image.ps1' }
    $proofPath = Join-Path $projectRoot '.cache/docker-image-validation.json'
    if (Test-Path -LiteralPath $proofPath) {
        $proof = Get-Content -LiteralPath $proofPath -Raw | ConvertFrom-Json
        if ($proof.gateway_image_id -eq $images[0].Id -and $proof.worker_image_id -eq $images[-1].Id) { $runtimeValidation = $proof }
    }
    $verification = [ordered]@{
        version = $builtVersion; packaged_at = [DateTimeOffset]::UtcNow.ToOffset([TimeSpan]::FromHours(8)).ToString('o'); platform = 'linux/amd64'
        images = @($images | ForEach-Object { @{ tags = $_.RepoTags; id = $_.Id } })
        image_archive_sha256 = $imageHash
        runtime_validation = $runtimeValidation
        limitations = 'Docker Desktop validation covers CPU. The worker includes GPU rendering and encoding software; host kernel/GPU drivers and NVIDIA Container Toolkit are external requirements. Intel and NVIDIA hardware require target-server acceptance.'
    }
    [IO.File]::WriteAllText("$packageRoot/VERIFICATION.json", ($verification | ConvertTo-Json -Depth 6), [Text.UTF8Encoding]::new($false))
    $zipPath = Join-Path $projectRoot "dist/$packageName-$releaseStamp.zip"
    if (Test-Path -LiteralPath $zipPath) { throw "Archive already exists: $zipPath" }
    $zip = [IO.Compression.ZipFile]::Open($zipPath, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($file in Get-ChildItem -LiteralPath $packageRoot -File) {
            $level = [IO.Compression.CompressionLevel]::Optimal
            if ($file.Name -eq 'images.tar') { $level = [IO.Compression.CompressionLevel]::NoCompression }
            [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $file.FullName, $file.Name, $level) | Out-Null
        }
    } finally { $zip.Dispose() }
    $zipHash = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText("$zipPath.sha256", "$zipHash  $([IO.Path]::GetFileName($zipPath))`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Built $zipPath ($versionText)"
} finally { Pop-Location }
