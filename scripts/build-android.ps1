. "$PSScriptRoot\env.ps1"
Push-Location "$taskRoot\core"
try {
    & go test ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'Core tests failed' }
    New-Item -ItemType Directory -Force "$taskRoot\app\libs" | Out-Null
    foreach ($abi in @('aarch64-linux-android','x86_64-linux-android')) {
        if (-not (Test-Path "$taskRoot\core\iroh\lib\$abi\libblizko_iroh.a")) { throw 'Build/download the iroh-native workflow libraries first' }
    }
    & gomobile bind '-target=android/arm64,android/amd64' -tags=iroh -androidapi 26 '-ldflags=-s -w' -o "$taskRoot\app\libs\mobile.aar" ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'iroh AAR build failed' }
} finally { Pop-Location }
Set-Content -LiteralPath "$taskRoot\local.properties" -Value "sdk.dir=$($env:ANDROID_HOME.Replace('\','/'))" -Encoding utf8
Push-Location $taskRoot
try {
    & "$taskRoot\.tools\gradle-8.11.1\bin\gradle.bat" --no-daemon :app:assembleDebug :app:lintDebug
    if ($LASTEXITCODE -ne 0) { throw 'Android build failed' }
    New-Item -ItemType Directory -Force "$taskRoot\dist" | Out-Null
    Copy-Item "$taskRoot\app\build\outputs\apk\debug\app-debug.apk" "$taskRoot\dist\Blizko-android.apk"
} finally { Pop-Location }
