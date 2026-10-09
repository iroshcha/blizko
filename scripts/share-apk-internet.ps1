$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$taskTunnel = Start-Process -FilePath "$taskRoot\.tools\cloudflared.exe" -ArgumentList @('tunnel','--url','http://127.0.0.1:8765','--no-autoupdate','--protocol','http2') -WindowStyle Hidden -PassThru -RedirectStandardError "$taskRoot\.tools\apk-tunnel.log" -RedirectStandardOutput "$taskRoot\.tools\apk-tunnel-out.log"
try {
    if (-not $taskTunnel.WaitForExit(7200000)) { $taskTunnel.Kill() }
} finally {
    if (-not $taskTunnel.HasExited) { $taskTunnel.Kill() }
}
