param([string]$TestDirectory = '')
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path $taskRoot '.tools'
$relayDirectory = Join-Path $toolRoot 'test-relay'
New-Item -ItemType Directory -Force -Path $relayDirectory | Out-Null
if ($TestDirectory -eq '') { $TestDirectory = Join-Path $toolRoot ('windows-network-' + [Guid]::NewGuid().ToString('N')) }
$TestDirectory = [IO.Path]::GetFullPath($TestDirectory)
New-Item -ItemType Directory -Force -Path $TestDirectory | Out-Null
$archive = Join-Path $relayDirectory 'relay.zip'
$digest = '2064308ca7df288d68eef89c80c194afe5038d85888c87079147dca862195a01'
if (-not (Test-Path -LiteralPath $archive)) {
    Invoke-WebRequest 'https://github.com/n0-computer/iroh/releases/download/v1.3.0/iroh-relay-v1.3.0-x86_64-pc-windows-msvc.zip' -OutFile $archive
}
if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $digest) { throw 'Test relay hash mismatch' }
Expand-Archive -LiteralPath $archive -DestinationPath $relayDirectory -Force
$config = Join-Path $relayDirectory 'config.toml'
[IO.File]::WriteAllText($config, 'http_bind_addr = "127.0.0.1:3340"' + [Environment]::NewLine + 'enable_metrics = false')
$previousRelay = $env:BLIZKO_RELAY_URL
$server = $null
try {
    $server = Start-Process -FilePath (Join-Path $relayDirectory 'iroh-relay.exe') -ArgumentList @('--dev','--config-path', ('"' + $config + '"')) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $TestDirectory 'relay.out') -RedirectStandardError (Join-Path $TestDirectory 'relay.err')
    Start-Sleep -Milliseconds 500
    if ($server.HasExited) { throw 'Local relay did not start; check relay.err and that port 3340 is free' }
    $env:BLIZKO_RELAY_URL = 'http://127.0.0.1:3340'
    $test = Start-Process -FilePath (Join-Path $taskRoot 'dist\windows\Blizko.exe') -ArgumentList @('--network-test', ('"' + $TestDirectory + '"')) -WindowStyle Hidden -PassThru -Wait
    Get-Content -LiteralPath (Join-Path $TestDirectory 'network-test.txt')
    if ($test.ExitCode -ne 0) { throw 'Windows process delivery test failed' }
} finally {
    $env:BLIZKO_RELAY_URL = $previousRelay
    if ($null -ne $server -and -not $server.HasExited) { Stop-Process -Id $server.Id }
}
