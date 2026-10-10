param([Parameter(Mandatory=$true)][string]$OutputDirectory)
$ErrorActionPreference='Stop'
$taskRoot=Split-Path -Parent $PSScriptRoot
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$framework=Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319'
$rsa=New-Object Security.Cryptography.RSACryptoServiceProvider(2048)
$rsa.PersistKeyInCsp=$false
[IO.File]::WriteAllText("$OutputDirectory\test-private.xml",$rsa.ToXmlString($true))
[IO.File]::WriteAllText("$OutputDirectory\UpdateKey.cs",'namespace Blizko { internal static class UpdateKey { internal const string PublicXml="'+$rsa.ToXmlString($false)+'"; } }')
$rsa.Dispose()
$refs=@('System.dll','System.Core.dll','System.Windows.Forms.dll','System.Web.Extensions.dll','System.Net.Http.dll','System.IO.Compression.dll','System.IO.Compression.FileSystem.dll') | ForEach-Object {"/reference:$framework\$_"}
$common=@('/nologo','/platform:x64','/utf8output')+$refs
foreach($version in @('old','new')){
 $options=@('/target:exe',"/out:$OutputDirectory\$version.exe")
 if($version -eq 'old'){$options+='/define:OLD'}
 & "$framework\csc.exe" @common @options "$taskRoot\windows\tests\UpdateFixture.cs"
 if($LASTEXITCODE -ne 0){throw 'Update fixture compilation failed'}
}
$sources=@("$taskRoot\windows\UpdatePackage.cs","$taskRoot\windows\WindowsUpdates.cs","$OutputDirectory\UpdateKey.cs")
# Keep the isolated fixture independent of a real Blizko instance on the host.
$helperSource=[IO.File]::ReadAllText("$taskRoot\windows\updater\Program.cs").Replace('Local\\Blizko.Windows.UI',('Local\\Blizko.UpdateFixture.'+[Guid]::NewGuid().ToString('N')))
[IO.File]::WriteAllText("$OutputDirectory\UpdaterFixture.cs",$helperSource)
& "$framework\csc.exe" @common /target:winexe "/out:$OutputDirectory\Blizko.Updater.exe" @sources "$OutputDirectory\UpdaterFixture.cs"
if($LASTEXITCODE -ne 0){throw 'Updater fixture compilation failed'}
Copy-Item -LiteralPath "$taskRoot\windows\Blizko.exe.config" -Destination "$OutputDirectory\Blizko.Updater.exe.config"
& "$framework\csc.exe" @common /target:exe "/out:$OutputDirectory\UpdateRegression.exe" @sources "$taskRoot\windows\tests\UpdateRegression.cs"
if($LASTEXITCODE -ne 0){throw 'Update regression compilation failed'}
$test=Start-Process -FilePath "$OutputDirectory\UpdateRegression.exe" -ArgumentList ('"'+$OutputDirectory+'"') -WindowStyle Hidden -PassThru -RedirectStandardOutput "$OutputDirectory\updates.txt" -RedirectStandardError "$OutputDirectory\update-errors.txt"
if(-not $test.WaitForExit(150000)){$test.Kill();throw 'Update regression timed out'}
Get-Content -LiteralPath "$OutputDirectory\updates.txt"
Get-Content -LiteralPath "$OutputDirectory\update-errors.txt"
if($test.ExitCode -ne 0){throw 'Windows update regression failed'}
