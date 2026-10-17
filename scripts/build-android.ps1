param([switch]$DebugBuild)
. "$PSScriptRoot\env.ps1"
Push-Location "$taskRoot\core"
try {
    & go test ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'Core tests failed' }
    New-Item -ItemType Directory -Force "$taskRoot\app\libs" | Out-Null
    & gomobile bind '-target=android/arm64,android/amd64' -androidapi 26 '-ldflags=-s -w' -o "$taskRoot\app\libs\mobile.aar" ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'Go AAR build failed' }
} finally { Pop-Location }
Set-Content -LiteralPath "$taskRoot\local.properties" -Value "sdk.dir=$($env:ANDROID_HOME.Replace('\','/'))" -Encoding utf8
Push-Location $taskRoot
try {
    $variant = if ($DebugBuild) { 'Debug' } else { 'Release' }
    & "$toolsRoot\gradle-8.11.1\bin\gradle.bat" --no-daemon ":app:assemble$variant" ":app:lint$variant"
    if ($LASTEXITCODE -ne 0) { throw 'Android build failed' }
    New-Item -ItemType Directory -Force "$taskRoot\dist" | Out-Null
    if ($DebugBuild) {
        Copy-Item "$taskRoot\app\build\outputs\apk\debug\app-debug.apk" "$taskRoot\dist\Blizko-android-debug.apk"
    } else {
        Copy-Item "$taskRoot\app\build\outputs\apk\release\app-release-unsigned.apk" "$taskRoot\dist\Blizko-android-unsigned.apk"
        Write-Output 'Release compiled with debugging disabled. Sign with the persistent Android key before distribution.'
    }
} finally { Pop-Location }
