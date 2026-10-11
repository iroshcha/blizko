$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$env:GOPATH = Join-Path $taskRoot '.tools\gopath'
$env:GOCACHE = Join-Path $taskRoot '.tools\gocache'
$env:GRADLE_USER_HOME = Join-Path $taskRoot '.tools\gradle-home'
if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = Join-Path $taskRoot '.tools\android-sdk' }
if (-not $env:ANDROID_NDK_HOME) { $env:ANDROID_NDK_HOME = Join-Path $env:ANDROID_HOME 'ndk\28.2.13676358' }
if (-not $env:JAVA_HOME) {
    $taskJdk = Get-ChildItem (Join-Path $taskRoot '.tools\jdk') -Directory | Select-Object -First 1
    $env:JAVA_HOME = $taskJdk.FullName
}
$taskTmp = Join-Path $taskRoot '.tools\tmp'
New-Item -ItemType Directory -Force $taskTmp | Out-Null
$env:TEMP = $taskTmp
$env:TMP = $taskTmp
$env:JAVA_TOOL_OPTIONS = "-Djdk.net.unixdomain.tmpdir=$taskTmp -Djava.io.tmpdir=$taskTmp"
$env:PATH = "$(Join-Path $taskRoot '.tools\go\bin');$(Join-Path $env:GOPATH 'bin');$(Join-Path $env:JAVA_HOME 'bin');$env:PATH"
