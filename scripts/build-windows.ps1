param([switch]$SkipNative, [switch]$SkipTests, [string]$RustTarget = '', [string]$ZxingDll = '')
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path $taskRoot '.tools'
if (Test-Path "$toolRoot\go\bin\go.exe") { $env:PATH = "$toolRoot\go\bin;$env:PATH" }
if (Test-Path "$toolRoot\cargo\bin\cargo.exe") {
    $env:RUSTUP_HOME = "$toolRoot\rustup"
    $env:CARGO_HOME = "$toolRoot\cargo"
    $env:PATH = "$toolRoot\cargo\bin;$env:PATH"
}
if (Test-Path "$toolRoot\w64devkit\bin\gcc.exe") {
    $env:PATH = "$toolRoot\w64devkit\bin;$env:PATH"
    # Recent w64devkit includes SEH unwinding in libgcc.a instead of a
    # separate libgcc_eh.a, while Rust's GNU target still requests the latter.
    $gccLibrary = & gcc -print-libgcc-file-name
    $gccEH = Join-Path (Split-Path -Parent $gccLibrary) 'libgcc_eh.a'
    if (-not (Test-Path -LiteralPath $gccEH)) { Copy-Item -LiteralPath $gccLibrary -Destination $gccEH }
}
$env:GOPATH = "$toolRoot\gopath"
$env:GOCACHE = "$toolRoot\gocache"
if ($RustTarget -eq '') {
    $RustTarget = ((& rustc -vV | Select-String '^host:').ToString() -replace '^host:\s*', '').Trim()
}
if ($RustTarget -notin @('x86_64-pc-windows-msvc','x86_64-pc-windows-gnu')) { throw 'Windows x64 Rust target required' }
if ($RustTarget -eq 'x86_64-pc-windows-msvc') { $env:RUSTFLAGS = "$env:RUSTFLAGS -C target-feature=+crt-static".Trim() }
if ($ZxingDll -eq '') { $ZxingDll = "$toolRoot\zxing\lib\net40\zxing.dll" }
if (-not (Test-Path -LiteralPath $ZxingDll)) { throw 'Download pinned ZXing.Net 0.16.11 and pass its net40 zxing.dll with -ZxingDll.' }
$out = Join-Path $taskRoot 'dist\windows'
New-Item -ItemType Directory -Force $out | Out-Null
Push-Location "$taskRoot\core\iroh"
try {
    if (-not $SkipNative) {
        & cargo rustc --release --locked --lib --crate-type cdylib --target $RustTarget
        if ($LASTEXITCODE -ne 0) { throw 'iroh Windows build failed' }
    }
    if (-not $SkipTests) {
        & cargo test --release --locked --lib --target $RustTarget
        if ($LASTEXITCODE -ne 0) { throw 'Windows transport tests failed' }
    }
    Copy-Item -LiteralPath "target\$RustTarget\release\blizko_iroh.dll" -Destination $out
} finally { Pop-Location }
$env:BLIZKO_IROH_DLL = Join-Path $out 'blizko_iroh.dll'
Push-Location "$taskRoot\core"
try {
    if (-not $SkipTests) {
        & go test -tags=iroh ./mobile ./desktop
        if ($LASTEXITCODE -ne 0) { throw 'Windows core tests failed' }
        & go vet -tags=iroh ./mobile ./desktop
        if ($LASTEXITCODE -ne 0) { throw 'Windows core vet failed' }
    }
    & go build -tags=iroh '-ldflags=-s -w' -o "$out\Blizko.Engine.exe" ./desktop
    if ($LASTEXITCODE -ne 0) { throw 'Windows engine build failed' }
} finally { Pop-Location }
$framework = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319'
& "$PSScriptRoot\windows-icon.ps1" -Path "$out\Blizko.ico"
$references = @('System.dll','System.Core.dll','System.Drawing.dll','System.Windows.Forms.dll','System.Web.Extensions.dll','System.Xaml.dll')
$compilerArgs = @('/nologo','/target:winexe','/platform:x64','/optimize+','/utf8output',"/out:$out\Blizko.exe", "/win32icon:$out\Blizko.ico", "/win32manifest:$taskRoot\windows\app.manifest", "/resource:$taskRoot\windows\MainWindow.xaml,Blizko.MainWindow.xaml", "/reference:$ZxingDll")
foreach ($reference in $references) { $compilerArgs += "/reference:$framework\$reference" }
foreach ($reference in @('PresentationCore.dll','PresentationFramework.dll','WindowsBase.dll')) { $compilerArgs += "/reference:$framework\WPF\$reference" }
$compilerArgs += @(Get-ChildItem -LiteralPath "$taskRoot\windows" -Filter '*.cs' | Select-Object -ExpandProperty FullName)
& "$framework\csc.exe" @compilerArgs
if ($LASTEXITCODE -ne 0) { throw 'Windows interface build failed' }
Copy-Item -LiteralPath $ZxingDll -Destination "$out\zxing.dll"
Copy-Item -LiteralPath "$taskRoot\windows\Blizko.exe.config" -Destination $out
Copy-Item -LiteralPath "$taskRoot\THIRD_PARTY_NOTICES.txt" -Destination $out
Copy-Item -LiteralPath "$taskRoot\app\src\main\assets\IROH_NOTICES.txt" -Destination $out
Copy-Item -LiteralPath "$taskRoot\windows\README.txt" -Destination $out
Copy-Item -LiteralPath "$taskRoot\windows\Blizko-Singapore.cmd" -Destination $out
Copy-Item -LiteralPath "$taskRoot\windows\ZXING_NET_NOTICE.txt" -Destination $out
Write-Output "Windows app: $out\Blizko.exe"
