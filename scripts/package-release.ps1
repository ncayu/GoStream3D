param([ValidateSet('linux', 'windows')][string[]]$Platforms = @('linux', 'windows'))
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'release-config.ps1')
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$distDir = Join-Path $projectRoot 'dist'
$releaseTimestamp = [TimeZoneInfo]::ConvertTimeBySystemTimeZoneId([DateTimeOffset]::UtcNow, 'China Standard Time').ToString('yyyyMMdd-HHmmss')
$releaseVersion = if ($env:GOSTREAM3D_RELEASE_VERSION) { $env:GOSTREAM3D_RELEASE_VERSION } else { $releaseTimestamp }
if ($releaseVersion -notmatch '^[A-Za-z0-9._-]+$') { throw 'GOSTREAM3D_RELEASE_VERSION contains unsupported characters' }
$releaseDir = Join-Path (Join-Path $distDir 'releases') $releaseTimestamp
if (Test-Path -LiteralPath $releaseDir) { throw "Release timestamp $releaseTimestamp already exists; retry in a second to preserve it." }
$archivePaths = @()

Push-Location -LiteralPath $projectRoot
try {
    Push-Location -LiteralPath (Join-Path $projectRoot 'web')
    try {
        npm ci
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
        npm test
        if ($LASTEXITCODE -ne 0) { throw 'web tests failed' }
        npm run build
        if ($LASTEXITCODE -ne 0) { throw 'web build failed' }
    } finally { Pop-Location }

    $assetDir = Join-Path $projectRoot 'internal/api/static/assets'
    New-Item -ItemType Directory -Force -Path $assetDir | Out-Null
    New-Item -ItemType Directory -Force -Path $distDir | Out-Null
    Get-ChildItem -LiteralPath $assetDir -Filter 'index-*' -File | Remove-Item
    Copy-Item -LiteralPath (Join-Path $projectRoot 'web/dist/index.html') -Destination (Join-Path $projectRoot 'internal/api/static/index.html') -Force
    Get-ChildItem -LiteralPath (Join-Path $projectRoot 'web/dist/assets') -File | Copy-Item -Destination $assetDir -Force

    # Browser profiles and archived releases under dist are not gateway source packages.
    go test ./cmd/... ./internal/...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }

    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    $oldCGO = $env:CGO_ENABLED
    try {
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        foreach ($platform in @($Platforms | Select-Object -Unique)) {
            $env:GOOS = $platform
            $suffix = if ($platform -eq 'windows') { '.exe' } else { '' }
            $archiveName = "GoStream3D-$platform-amd64"
            # Keep release staging separate from running installations and their passwords/data.
            $packageDir = Join-Path $releaseDir $archiveName
            New-Item -ItemType Directory -Force -Path $packageDir | Out-Null
            $binaryPath = Join-Path $packageDir "gostream3d$suffix"
            go build -trimpath -ldflags "-s -w -X main.version=$releaseVersion" -o $binaryPath ./cmd/server
            if ($LASTEXITCODE -ne 0) { throw "$platform Go build failed" }

            Copy-Item -LiteralPath (Join-Path $projectRoot "config.$platform.example.env") -Destination (Join-Path $packageDir 'config.example.env') -Force
            New-ReleaseConfig -TemplatePath (Join-Path $projectRoot "config.$platform.example.env") -Destination (Join-Path $packageDir 'config.env')
            Copy-Item -LiteralPath (Join-Path $projectRoot 'docs/release-parameters.md') -Destination (Join-Path $packageDir 'PARAMETERS.md') -Force
            Copy-Item -LiteralPath (Join-Path $projectRoot 'docs/binary-release.md') -Destination (Join-Path $packageDir 'README.md') -Force
            Copy-Item -LiteralPath (Join-Path $projectRoot 'docs/admin-account.md') -Destination (Join-Path $packageDir 'ADMIN-ACCOUNT.md') -Force
            if ($platform -eq 'linux') {
                $startScript = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'start-linux.sh')).Replace("`r`n", "`n")
                [IO.File]::WriteAllText((Join-Path $packageDir 'start.sh'), $startScript, [Text.UTF8Encoding]::new($false))
                Copy-Item -LiteralPath (Join-Path $projectRoot 'docs/native-linux.md') -Destination (Join-Path $packageDir 'NATIVE-LINUX.md') -Force
                Copy-Item -LiteralPath (Join-Path $projectRoot 'docs/offline-dependencies.md') -Destination (Join-Path $packageDir 'OFFLINE-DEPENDENCIES.md') -Force
            } else {
                # Windows PowerShell 5.1 needs a BOM for Chinese text; CMD needs CRLF.
                $launcher = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'start-windows.bat')).Replace("`r`n", "`n").Replace("`n", "`r`n")
                [IO.File]::WriteAllText((Join-Path $packageDir 'start.bat'), $launcher, [Text.Encoding]::ASCII)
                $helper = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'start-windows.ps1')).Replace("`r`n", "`n").Replace("`n", "`r`n")
                [IO.File]::WriteAllText((Join-Path $packageDir 'start-windows.ps1'), $helper, [Text.UTF8Encoding]::new($true))
            }

            $zipPath = Join-Path $distDir "$archiveName-$releaseTimestamp.zip"
            if (Test-Path -LiteralPath $zipPath) { throw "Archive already exists: $zipPath" }
            $files = @($binaryPath, (Join-Path $packageDir 'config.env'), (Join-Path $packageDir 'config.example.env'), (Join-Path $packageDir 'PARAMETERS.md'), (Join-Path $packageDir 'README.md'), (Join-Path $packageDir 'ADMIN-ACCOUNT.md'))
            if ($platform -eq 'linux') { $files += (Join-Path $packageDir 'start.sh'); $files += (Join-Path $packageDir 'NATIVE-LINUX.md'); $files += (Join-Path $packageDir 'OFFLINE-DEPENDENCIES.md') }
            else { $files += (Join-Path $packageDir 'start.bat'); $files += (Join-Path $packageDir 'start-windows.ps1') }
            for ($attempt = 1; $attempt -le 5; $attempt++) {
                try {
                    Compress-Archive -LiteralPath $files -DestinationPath $zipPath -Force -ErrorAction Stop
                    break
                } catch {
                    if ($attempt -eq 5) { throw }
                    Start-Sleep -Seconds 2
                }
            }
            Write-Host "Built $zipPath"
            $archivePaths += $zipPath
        }
    } finally {
        $env:GOOS = $oldGOOS
        $env:GOARCH = $oldGOARCH
        $env:CGO_ENABLED = $oldCGO
    }

    $hashes = Get-FileHash -Algorithm SHA256 -LiteralPath $archivePaths
    $checksumPath = Join-Path $distDir "SHA256SUMS-$releaseTimestamp.txt"
    $hashes | ForEach-Object { "$($_.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($_.Path))" } | Set-Content -LiteralPath $checksumPath -Encoding ascii
    @{
        version = $releaseVersion
        timestamp = $releaseTimestamp
        timezone = 'Asia/Shanghai'
        archives = $archivePaths
        checksums = $checksumPath
    } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $releaseDir 'RELEASE.json') -Encoding utf8
} finally { Pop-Location }
