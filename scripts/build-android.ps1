. "$PSScriptRoot\env.ps1"
Push-Location "$taskRoot\core"
try {
    & go test -tags=ts_omit_logtail ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'Core tests failed' }
    New-Item -ItemType Directory -Force "$taskRoot\app\libs" | Out-Null
    & gomobile bind '-target=android/arm64,android/amd64' -tags=ts_omit_logtail -androidapi 26 '-ldflags=-s -w' -o "$taskRoot\app\libs\mobile.aar" ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'Tailscale AAR build failed' }
} finally { Pop-Location }
Set-Content -LiteralPath "$taskRoot\local.properties" -Value "sdk.dir=$($env:ANDROID_HOME.Replace('\','/'))" -Encoding utf8
Push-Location $taskRoot
try {
    & "$taskRoot\.tools\gradle-8.11.1\bin\gradle.bat" --no-daemon :app:assembleDebug :app:lintDebug
    if ($LASTEXITCODE -ne 0) { throw 'Android build failed' }
    New-Item -ItemType Directory -Force "$taskRoot\dist" | Out-Null
    Copy-Item "$taskRoot\app\build\outputs\apk\debug\app-debug.apk" "$taskRoot\dist\Blizko-android.apk"
} finally { Pop-Location }
