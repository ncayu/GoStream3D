# Create distributable credentials separately from any local running installation.
function New-ReleaseConfig {
    param(
        [Parameter(Mandatory = $true)][string]$TemplatePath,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    $content = [IO.File]::ReadAllText($TemplatePath)
    $content = [Regex]::Replace($content, '(?m)^GOSTREAM3D_ADMIN_PASSWORD=.*$', 'GOSTREAM3D_ADMIN_PASSWORD=Admin@2026.com')
    $credentialKeys = @('GOSTREAM3D_NEKO_USER_PASSWORD', 'GOSTREAM3D_NEKO_ADMIN_PASSWORD')
    $randomSource = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        foreach ($key in $credentialKeys) {
            $pattern = '(?m)^' + [Regex]::Escape($key) + '=.*$'
            if ([Regex]::IsMatch($content, $pattern)) {
                $secretBytes = New-Object byte[] 24
                $randomSource.GetBytes($secretBytes)
                $secretValue = [BitConverter]::ToString($secretBytes).Replace('-', '').ToLowerInvariant()
                $content = [Regex]::Replace($content, $pattern, $key + '=' + $secretValue)
            }
        }
    } finally { $randomSource.Dispose() }
    [IO.File]::WriteAllText($Destination, $content, (New-Object Text.UTF8Encoding($false)))
}
