param([switch]$SkipTests, [string]$ZxingDll = '', [string]$ToolRoot = '')
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
if (-not $ToolRoot) { $ToolRoot = Join-Path $taskRoot '.tools' }
if (Test-Path "$toolRoot\go\bin\go.exe") { $env:PATH = "$toolRoot\go\bin;$env:PATH" }
$env:GOPATH = "$toolRoot\gopath"
$env:GOCACHE = "$toolRoot\gocache"
if ($ZxingDll -eq '') { $ZxingDll = "$toolRoot\zxing\lib\net40\zxing.dll" }
if (-not (Test-Path -LiteralPath $ZxingDll)) { throw 'Download pinned ZXing.Net 0.16.11 and pass its net40 zxing.dll with -ZxingDll.' }
$out = Join-Path $taskRoot 'dist\windows'
New-Item -ItemType Directory -Force $out | Out-Null
Push-Location "$taskRoot\core"
try {
    if (-not $SkipTests) {
        & go test ./...
        if ($LASTEXITCODE -ne 0) { throw 'Windows core tests failed' }
        & go vet ./...
        if ($LASTEXITCODE -ne 0) { throw 'Windows core vet failed' }
    }
    & go build '-ldflags=-s -w' -o "$out\Blizko.Engine.exe" ./desktop
    if ($LASTEXITCODE -ne 0) { throw 'Windows engine build failed' }
    & go build '-ldflags=-s -w' -o "$taskRoot\dist\Blizko.Server.exe" ./server
    if ($LASTEXITCODE -ne 0) { throw 'Server build failed' }
} finally { Pop-Location }
$framework = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319'
& "$PSScriptRoot\windows-icon.ps1" -Path "$out\Blizko.ico"
$references = @('System.dll','System.Core.dll','System.Drawing.dll','System.Windows.Forms.dll','System.Web.Extensions.dll','System.Xaml.dll','System.Net.Http.dll','System.IO.Compression.dll','System.IO.Compression.FileSystem.dll')
$compilerArgs = @('/nologo','/target:winexe','/platform:x64','/optimize+','/utf8output',"/out:$out\Blizko.exe", "/win32icon:$out\Blizko.ico", "/win32manifest:$taskRoot\windows\app.manifest", "/resource:$taskRoot\windows\MainWindow.xaml,Blizko.MainWindow.xaml", "/reference:$ZxingDll")
foreach ($reference in $references) { $compilerArgs += "/reference:$framework\$reference" }
foreach ($reference in @('PresentationCore.dll','PresentationFramework.dll','WindowsBase.dll')) { $compilerArgs += "/reference:$framework\WPF\$reference" }
$compilerArgs += @(Get-ChildItem -LiteralPath "$taskRoot\windows" -Filter '*.cs' | Select-Object -ExpandProperty FullName)
& "$framework\csc.exe" @compilerArgs
if ($LASTEXITCODE -ne 0) { throw 'Windows interface build failed' }
$updaterArgs=@('/nologo','/target:winexe','/platform:x64','/optimize+','/utf8output',"/out:$out\Blizko.Updater.exe", "/win32manifest:$taskRoot\windows\app.manifest")
foreach($reference in $references){$updaterArgs+="/reference:$framework\$reference"}
$updaterArgs+=@("$taskRoot\windows\updater\Program.cs","$taskRoot\windows\UpdatePackage.cs","$taskRoot\windows\UpdateKey.cs","$taskRoot\windows\WindowsUpdates.cs","$taskRoot\windows\AssemblyInfo.cs")
& "$framework\csc.exe" @updaterArgs
if($LASTEXITCODE -ne 0){throw 'Windows updater build failed'}
Copy-Item -LiteralPath "$taskRoot\windows\Blizko.exe.config" -Destination "$out\Blizko.Updater.exe.config"

Copy-Item -LiteralPath $ZxingDll -Destination "$out\zxing.dll"
Copy-Item -LiteralPath "$taskRoot\windows\Blizko.exe.config" -Destination $out
Copy-Item -LiteralPath "$taskRoot\THIRD_PARTY_NOTICES.txt" -Destination $out
& "$PSScriptRoot\windows-compat.ps1" -OutputDirectory $out
Copy-Item -LiteralPath "$taskRoot\windows\README.txt" -Destination $out
$oldLauncher = Join-Path $out 'Blizko-Singapore.cmd'
if (Test-Path -LiteralPath $oldLauncher) { Remove-Item -LiteralPath $oldLauncher }
Copy-Item -LiteralPath "$taskRoot\windows\ZXING_NET_NOTICE.txt" -Destination $out
Write-Output "Windows app: $out\Blizko.exe"
