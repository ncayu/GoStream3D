param([switch]$NoBrowser)
$ErrorActionPreference = 'Stop'

try {
    $directory = $PSScriptRoot
    $executable = Join-Path $directory 'gostream3d.exe'
    if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
        throw '未找到 gostream3d.exe，请完整解压安装包后再运行 start.bat。'
    }
    $settings = @{}
    $configPath = Join-Path $directory 'config.env'
    if (Test-Path -LiteralPath $configPath -PathType Leaf) {
        foreach ($line in [IO.File]::ReadAllLines($configPath, [Text.Encoding]::UTF8)) {
            if ($line -match '^\s*(GOSTREAM3D_[A-Z0-9_]+)\s*=(.*)$') {
                $value = $matches[2].Trim()
                if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
                    $value = $value.Substring(1, $value.Length - 2)
                }
                $settings[$matches[1]] = $value
            }
        }
    }
    function Setting([string]$Name, [string]$Fallback) {
        $environmentValue = [Environment]::GetEnvironmentVariable($Name)
        if ($environmentValue) { return $environmentValue }
        if ($settings.ContainsKey($Name)) { return $settings[$Name] }
        return $Fallback
    }
    $address = Setting 'GOSTREAM3D_HTTP_ADDR' ':8888'
    if ($address -notmatch '^(.*):(\d+)$') { throw 'GOSTREAM3D_HTTP_ADDR 格式不正确。' }
    $bindHost = $matches[1].Trim('[', ']')
    $defaultPort = $matches[2]
    $bindHost = Setting 'GOSTREAM3D_HTTP_HOST' $bindHost
    $port = [int](Setting 'GOSTREAM3D_HTTP_PORT' $defaultPort)
    if ($port -lt 1 -or $port -gt 65535) { throw '监听端口必须在 1–65535 范围内。' }
    $browserHost = $bindHost
    if ($browserHost -in @('', '0.0.0.0', '::')) { $browserHost = '127.0.0.1' }

    $running = @(Get-Process -Name gostream3d -ErrorAction SilentlyContinue | Where-Object {
        try { $_.Path -eq $executable } catch { $false }
    })
    $started = $false
    if ($running.Count -gt 0) {
        $server = $running[0]
        $listeners = @(Get-NetTCPConnection -State Listen -OwningProcess $server.Id -ErrorAction SilentlyContinue)
        if ($listeners.Count -gt 0) { $port = [int]$listeners[0].LocalPort }
        Write-Host '程序已在运行，正在打开管理页面。'
    } else {
        $configuredPort = $port
        $available = $false
        $probeAddress = [Net.IPAddress]::Any
        if ($bindHost -and $bindHost -ne '0.0.0.0') { $probeAddress = [Net.IPAddress]::Parse($bindHost) }
        foreach ($candidate in @($configuredPort, 10088, 18888, 28888) | Select-Object -Unique) {
            $probe = [Net.Sockets.TcpListener]::new($probeAddress, $candidate)
            try {
                $probe.Start()
                $port = $candidate
                $available = $true
                break
            } catch {
            } finally { $probe.Stop() }
        }
        if (-not $available) { throw '没有可用端口，请修改 config.env 的 GOSTREAM3D_HTTP_PORT 后重试。' }
        if ($port -ne $configuredPort) {
            Write-Host "端口 $configuredPort 不可用，本次改用 $port；原配置文件保留。"
        }
        $env:GOSTREAM3D_HTTP_PORT = [string]$port
        $env:GOSTREAM3D_NO_BROWSER = '1'
        $server = Start-Process -FilePath $executable -WorkingDirectory $directory -WindowStyle Hidden -PassThru
        $started = $true
    }
    $base = [UriBuilder]::new('http', $browserHost, $port).Uri.AbsoluteUri.TrimEnd('/')
    $ready = $false
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $health = Invoke-RestMethod -Uri "$base/healthz" -TimeoutSec 2
            if ($health.status -eq 'ok') { $ready = $true; break }
        } catch {}
        if ($server.HasExited) { break }
        Start-Sleep -Milliseconds 250
        $server.Refresh()
    }
    if (-not $ready) {
        if ($started -and -not $server.HasExited) { Stop-Process -Id $server.Id -Force }
        throw '程序启动检查未通过，请查看安装目录中的 gostream3d.log。'
    }
    $url = "$base/admin"
    Write-Host "管理页面：$url"
    Write-Host '服务在后台运行；关闭此启动窗口不影响服务。'
    if (-not $NoBrowser) { Start-Process -FilePath $url | Out-Null }
    exit 0
} catch {
    Write-Host "启动失败：$($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
