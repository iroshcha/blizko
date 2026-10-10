param([Parameter(Mandatory=$true)][string]$OutputDirectory,[string]$ZxingDll='')
$ErrorActionPreference='Stop'
$taskRoot=Split-Path -Parent $PSScriptRoot
if($ZxingDll -eq ''){$ZxingDll=Join-Path $taskRoot '.tools\zxing\lib\net40\zxing.dll'}
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$framework=Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319'
& "$framework\csc.exe" /nologo /target:exe /platform:x64 "/out:$OutputDirectory\Blizko.Engine.exe" "/reference:$framework\System.Web.Extensions.dll" "$taskRoot\windows\tests\FakeEngine.cs"
if($LASTEXITCODE -ne 0){throw 'Fixture compilation failed'}
$compilerArgs=@('/nologo','/target:exe','/platform:x64','/utf8output','/main:InteractionRegression',"/out:$OutputDirectory\InteractionRegression.exe", "/resource:$taskRoot\windows\MainWindow.xaml,Blizko.MainWindow.xaml", "/reference:$ZxingDll")
foreach($reference in @('System.dll','System.Core.dll','System.Drawing.dll','System.Windows.Forms.dll','System.Web.Extensions.dll','System.Xaml.dll')){$compilerArgs+="/reference:$framework\$reference"}
foreach($reference in @('PresentationCore.dll','PresentationFramework.dll','WindowsBase.dll')){$compilerArgs+="/reference:$framework\WPF\$reference"}
$compilerArgs+=@(Get-ChildItem -LiteralPath "$taskRoot\windows" -Filter '*.cs' | Select-Object -ExpandProperty FullName)
$compilerArgs+="$taskRoot\windows\tests\InteractionRegression.cs"
& "$framework\csc.exe" @compilerArgs
if($LASTEXITCODE -ne 0){throw 'UI regression compilation failed'}
Copy-Item -LiteralPath $ZxingDll -Destination $OutputDirectory
Copy-Item -LiteralPath "$taskRoot\windows\Blizko.exe.config" -Destination "$OutputDirectory\InteractionRegression.exe.config"
& "$OutputDirectory\InteractionRegression.exe" | Tee-Object -FilePath "$OutputDirectory\interactions.txt"
if($LASTEXITCODE -ne 0){throw 'UI interaction regression failed'}
