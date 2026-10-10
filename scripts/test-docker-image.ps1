# Runs against a local development Docker engine, using only disposable test data.
param([switch]$StandaloneCompose)
$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Push-Location -LiteralPath $projectRoot
function Invoke-TestDocker([string[]]$DockerArgs) {
    if ($StandaloneCompose -and $DockerArgs[0] -eq 'compose') {
        $standaloneArgs = $DockerArgs[1..($DockerArgs.Length - 1)]
        $result = & docker-compose @standaloneArgs
    } else { $result = & docker @DockerArgs }
    if ($LASTEXITCODE -ne 0) { throw "Docker command failed: $($DockerArgs[0])" }
    return $result
}
$project = 'gostream3d-check-' + (Get-Date -Format 'yyyyMMddHHmmss')
$siteName = "$project-site"
$testRoot = Join-Path $projectRoot ".cache/$project"
$gatewayID = ''
try {
    $managed = Invoke-TestDocker @('ps', '-q', '--filter', 'label=gostream3d.managed=true')
    if ($managed) { throw 'Stop existing managed workers before this opt-in development test; gateway startup cleans stale workers.' }
    New-Item -ItemType Directory -Path "$testRoot/data", "$testRoot/site", "$testRoot/host-root" -Force | Out-Null
    # Docker Desktop's private VM root cannot be propagated. Exercise the same
    # host mapping with a fixture here; real Linux disks are verified on the server.
    [IO.File]::WriteAllText("$testRoot/mountinfo", "1 0 8:1 / / rw - ext4 /dev/test-host-disk rw`n", [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText("$testRoot/site/index.html", '<html><body id="ready"><canvas id="c" width="640" height="360"></canvas><script>const g=c.getContext("webgl",{preserveDrawingBuffer:true});g.clearColor(0.1,0.6,0.2,1);g.clear(g.COLOR_BUFFER_BIT);</script></body></html>', [Text.UTF8Encoding]::new($false))
    $override = @'
services:
  gateway:
    environment:
      GOSTREAM3D_HTTP_PORTS: 43280-43280
      GOSTREAM3D_CDP_PORTS: 43281-43281
      GOSTREAM3D_UDP_PORTS: 45240-45247
'@
    $override += @"

    volumes:
      - type: bind
        source: $($testRoot.Replace('\', '/'))/host-root
        target: /host/root
        read_only: true
        bind:
          propagation: rprivate
          create_host_path: false
      - type: bind
        source: $($testRoot.Replace('\', '/'))/mountinfo
        target: /host/mountinfo
        read_only: true
        bind:
          create_host_path: false
"@
    [IO.File]::WriteAllText("$testRoot/override.yml", $override.Replace("`r`n", "`n"), [Text.UTF8Encoding]::new($false))
    $env:GOSTREAM3D_GATEWAY_IMAGE = 'gostream3d/gateway:latest'
    $env:GOSTREAM3D_ICE_PUBLIC_IP = '127.0.0.1'
    $env:GOSTREAM3D_HTTP_PORT = '18888'
    $env:GOSTREAM3D_DOCKER_SOCKET = '/var/run/docker.sock'
    $env:GOSTREAM3D_DATA_DIR = "$testRoot/data"
    $composeArgs = @('compose', '-p', $project, '-f', "$projectRoot/docker-compose.yml", '-f', "$testRoot/override.yml")
    Invoke-TestDocker (@('run', '-d', '--name', $siteName, '--mount', "type=bind,source=$testRoot/site,target=/site,readonly", '--entrypoint', 'python3', 'gostream3d/neko/chromium:3.1.5', '-m', 'http.server', '8089', '--directory', '/site')) | Out-Null
    $siteIP = (Invoke-TestDocker @('inspect', '--format', '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}', $siteName)).Trim()
    Invoke-TestDocker ($composeArgs + @('up', '-d', '--wait', '--wait-timeout', '90', '--pull', 'never')) | Out-Null
    $gatewayID = (Invoke-TestDocker ($composeArgs + @('ps', '-q', 'gateway'))).Trim()
    function Invoke-GatewayRequest([string]$Method, [string]$Path, [string]$Body = '') {
        $curlArgs = @('exec', '-i', $gatewayID, 'curl', '-fsS', '--max-time', '105', '-c', '/tmp/check.cookies', '-b', '/tmp/check.cookies', '-X', $Method, '-H', 'Origin: http://127.0.0.1:18888')
        if ($Body) {
            $response = $Body | & docker @curlArgs -H 'Content-Type: application/json' --data-binary '@-' "http://127.0.0.1:18888$Path"
        } else { $response = & docker @curlArgs "http://127.0.0.1:18888$Path" }
        if ($LASTEXITCODE -ne 0) { throw "Gateway request failed: $Method $Path" }
        return ($response -join "`n")
    }
    function Invoke-GatewayStatus([string]$Method, [string]$Path, [string]$Body = '', [string]$CookieFile = '/tmp/check.cookies') {
        $curlArgs = @('exec', '-i', $gatewayID, 'curl', '-sS', '--max-time', '15', '-o', '/dev/null', '-w', '%{http_code}', '-b', $CookieFile, '-X', $Method, '-H', 'Origin: http://127.0.0.1:18888')
        if ($Body) { $response = $Body | & docker @curlArgs -H 'Content-Type: application/json' --data-binary '@-' "http://127.0.0.1:18888$Path" }
        else { $response = & docker @curlArgs "http://127.0.0.1:18888$Path" }
        if ($LASTEXITCODE -ne 0) { throw "Gateway status check failed: $Method $Path" }
        return ($response -join '').Trim()
    }
    $configPath = "$testRoot/data/config.env"
    $configHash = (Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash
    $passwordLine = Get-Content -LiteralPath $configPath | Where-Object { $_.StartsWith('GOSTREAM3D_ADMIN_PASSWORD=') }
    $password = $passwordLine.Substring('GOSTREAM3D_ADMIN_PASSWORD='.Length)
    $mode = Invoke-TestDocker @('exec', $gatewayID, 'stat', '-c', '%a', '/data/config.env')
    if ($mode.Trim() -ne '600') { throw 'Configuration permissions are not 0600' }
    $adminHTML = Invoke-GatewayRequest 'GET' '/admin'
    $freshIndex = Get-Content -LiteralPath web/dist/index.html -Raw
    $freshAsset = [regex]::Match($freshIndex, '/assets/index-[^" ]+\.js').Value
    if (-not $freshAsset -or -not $adminHTML.Contains($freshAsset)) { throw 'Gateway does not serve the latest frontend' }
    $loginBody = @{ username = 'admin'; password = $password } | ConvertTo-Json -Compress
    Invoke-GatewayRequest 'POST' '/api/v1/auth/login' $loginBody | Out-Null
    $monitor = Invoke-GatewayRequest 'POST' '/api/v1/admin/monitor/refresh' | ConvertFrom-Json
    if ($monitor.data.partitions.Count -lt 1 -or $monitor.data.disk.total_bytes -le 0) { throw 'Host disk monitoring returned no readable partitions' }
    if ($monitor.data.errors -match '磁盘') { throw 'Host disk monitoring reported an error' }
    $sessionBody = @{ name = 'Docker release test'; target_url = "http://${siteIP}:8089/"; render_mode = 'cpu'; resolution = 'adaptive'; fps = 30; ready_selector = '#ready'; concurrency_mode = 'single'; max_viewers = 1; preload_count = 1 } | ConvertTo-Json -Compress
    $created = Invoke-GatewayRequest 'POST' '/api/v1/sessions' $sessionBody | ConvertFrom-Json
    if (-not $created.data.preloaded -or -not $created.data.token) { throw 'CPU rendering project failed to preload' }
    $token = $created.data.token
    $metadata = Invoke-GatewayRequest 'GET' "/api/v1/sessions/$token" | ConvertFrom-Json
    if ($metadata.data.max_width -ne 3840 -or $metadata.data.max_height -ne 2160) { throw 'CPU adaptive resolution is unexpectedly reduced' }
    $workerID = (Invoke-TestDocker @('ps', '-q', '--filter', "name=gostream3d-$token")).Trim()
    if (-not $workerID) { throw 'Preloaded worker is missing' }
    $worker = (Invoke-TestDocker @('inspect', $workerID) | ConvertFrom-Json)[0]
    if ($worker.Config.Env -notcontains 'NEKO_SCREEN=1920x1080@30') { throw 'CPU preloading reduced the configured screen' }
    $screenProbe = 'import ctypes; x=ctypes.CDLL("libX11.so.6"); x.XOpenDisplay.argtypes=[ctypes.c_char_p]; x.XOpenDisplay.restype=ctypes.c_void_p; x.XDisplayWidth.argtypes=[ctypes.c_void_p,ctypes.c_int]; x.XDisplayHeight.argtypes=[ctypes.c_void_p,ctypes.c_int]; x.XCloseDisplay.argtypes=[ctypes.c_void_p]; d=x.XOpenDisplay(b":99.0"); assert d; print(str(x.XDisplayWidth(d,0))+"x"+str(x.XDisplayHeight(d,0))); x.XCloseDisplay(d)'
    $actualScreen = (Invoke-TestDocker @('exec', $workerID, 'python3', '-c', $screenProbe)).Trim()
    if ($actualScreen -ne '1920x1080') { throw "CPU adaptive preload actually rendered at $actualScreen" }
    $pipelineEnv = $worker.Config.Env | Where-Object { $_.StartsWith('NEKO_CAPTURE_VIDEO_PIPELINES=') }
    if (-not $pipelineEnv) { throw 'Explicit main/legacy pipelines are missing' }
    $pipelines = $pipelineEnv.Substring('NEKO_CAPTURE_VIDEO_PIPELINES='.Length) | ConvertFrom-Json
    foreach ($streamID in @('main', 'legacy')) {
        if (-not $pipelines.$streamID.gst_pipeline.Contains('vp8enc threads=2 cpu-used=8') -or -not $pipelines.$streamID.gst_pipeline.Contains('show-pointer=false')) { throw "CPU encoding or single-cursor configuration missing in $streamID" }
    }
    $viewer = Invoke-GatewayRequest 'GET' "/Stream3D/$token"
    if (-not $viewer.Contains($freshAsset)) { throw 'Viewer page failed' }
    Invoke-GatewayRequest 'POST' "/api/v1/admin/sessions/$token/stop" | Out-Null
    Invoke-TestDocker ($composeArgs + @('restart', 'gateway')) | Out-Null
    Invoke-TestDocker ($composeArgs + @('up', '-d', '--wait', '--wait-timeout', '60', '--pull', 'never')) | Out-Null
    Invoke-GatewayRequest 'POST' '/api/v1/auth/login' $loginBody | Out-Null
    $saved = Invoke-GatewayRequest 'GET' '/api/v1/admin/sessions' | ConvertFrom-Json
    $savedProject = $saved.data | Where-Object { $_.token -eq $token }
    if (-not $savedProject -or -not $savedProject.render_paused) { throw 'Project did not survive gateway restart' }
    if ((Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash -ne $configHash) { throw 'Restart replaced configuration credentials' }
    $me = Invoke-GatewayRequest 'GET' '/api/v1/auth/me' | ConvertFrom-Json
    if (-not $me.account_update_supported) { throw 'Administrator account editing is unavailable' }
    Invoke-TestDocker @('exec', $gatewayID, 'cp', '/tmp/check.cookies', '/tmp/old.cookies') | Out-Null
    $newUsername = 'release-operator'
    $newPassword = [Guid]::NewGuid().ToString('N')
    $updateBody = @{ username = $newUsername; current_password = $password; new_password = $newPassword } | ConvertTo-Json -Compress
    Invoke-GatewayRequest 'PUT' '/api/v1/auth/account' $updateBody | Out-Null
    if ((Invoke-GatewayStatus 'GET' '/api/v1/auth/me' '' '/tmp/old.cookies') -ne '401') { throw 'Account update did not revoke the previous login session' }
    if ((Invoke-GatewayStatus 'POST' '/api/v1/auth/login' $loginBody) -ne '401') { throw 'Initial password still grants access' }
    $newLoginBody = @{ username = $newUsername; password = $newPassword } | ConvertTo-Json -Compress
    Invoke-GatewayRequest 'POST' '/api/v1/auth/login' $newLoginBody | Out-Null
    $me = Invoke-GatewayRequest 'GET' '/api/v1/auth/me' | ConvertFrom-Json
    if ($me.username -ne $newUsername) { throw 'Updated account name did not take effect' }
    $storeLine = Get-Content -LiteralPath $configPath | Where-Object { $_.StartsWith('GOSTREAM3D_SESSION_STORE=') }
    $containerStore = if ($storeLine) { $storeLine.Substring('GOSTREAM3D_SESSION_STORE='.Length) } else { 'data/sessions.json' }
    if (-not $containerStore.StartsWith('/')) { $containerStore = '/data/' + $containerStore }
    $accountContainerPath = $containerStore.Substring(0, $containerStore.LastIndexOf('/')) + '/admin-account.json'
    if (-not $accountContainerPath.StartsWith('/data/')) { throw 'Account storage escaped the persistent data mount' }
    $accountHostPath = Join-Path "$testRoot/data" $accountContainerPath.Substring('/data/'.Length)
    $accountJSON = [IO.File]::ReadAllText($accountHostPath)
    if ($accountJSON.Contains($newPassword) -or $accountJSON.Contains($password)) { throw 'Plaintext password was saved in the account file' }
    if ((Invoke-TestDocker @('exec', $gatewayID, 'stat', '-c', '%a', $accountContainerPath)).Trim() -ne '600') { throw 'Account file permissions are not 0600' }
    Invoke-TestDocker ($composeArgs + @('up', '-d', '--force-recreate', '--wait', '--wait-timeout', '90', '--pull', 'never')) | Out-Null
    $previousGatewayID = $gatewayID
    $gatewayID = (Invoke-TestDocker ($composeArgs + @('ps', '-q', 'gateway'))).Trim()
    if ($gatewayID -eq $previousGatewayID) { throw 'Account persistence test did not recreate the gateway container' }
    if ((Invoke-GatewayStatus 'POST' '/api/v1/auth/login' $loginBody) -ne '401') { throw 'Container recreation restored the initial password' }
    Invoke-GatewayRequest 'POST' '/api/v1/auth/login' $newLoginBody | Out-Null
    $me = Invoke-GatewayRequest 'GET' '/api/v1/auth/me' | ConvertFrom-Json
    if ($me.username -ne $newUsername) { throw 'Updated account did not survive container recreation' }
    $saved = Invoke-GatewayRequest 'GET' '/api/v1/admin/sessions' | ConvertFrom-Json
    if (-not ($saved.data | Where-Object { $_.token -eq $token -and $_.render_paused })) { throw 'Account changes lost the existing project' }
    if ((Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash -ne $configHash) { throw 'Account changes replaced the initial configuration' }
    $proof = [ordered]@{
        status = 'passed'; tested_at = (Get-Date -Format 'yyyy-MM-ddTHH:mm:sszzz')
        compose_command = $(if ($StandaloneCompose) { 'docker-compose' } else { 'docker compose' })
        gateway_image_id = (Invoke-TestDocker @('image', 'inspect', '--format', '{{.Id}}', 'gostream3d/gateway:latest')).Trim()
        worker_image_id = (Invoke-TestDocker @('image', 'inspect', '--format', '{{.Id}}', 'gostream3d/neko/chromium:3.1.5')).Trim()
        checks = @('latest frontend', 'automatic 0600 configuration', 'administrator login', 'host disk mount mapping and nonzero capacity (fixture)', 'Docker socket', 'CPU preload at full 1920x1080', 'CPU adaptive limits restored to 3840x2160', 'optimized CPU video pipeline', 'viewer page', 'graceful stop', 'project and password persistence after restart', 'administrator account and password update', 'old password and session invalidation', 'salted password hash and 0600 account file', 'new account persistence after container recreation', 'project preserved after account update')
    }
    [IO.File]::WriteAllText((Join-Path $projectRoot '.cache/docker-image-validation.json'), ($proof | ConvertTo-Json -Depth 4), [Text.UTF8Encoding]::new($false))
    Write-Output 'PASS: latest UI, CPU preload, persisted project, administrator account rotation, old login invalidation, and new account persistence after container recreation.'
} finally {
    if ($composeArgs) { Invoke-TestDocker ($composeArgs + @('down', '--timeout', '45')) | Out-Null }
    & docker rm -f $siteName 2>$null | Out-Null
    Pop-Location
}
