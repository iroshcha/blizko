param([string]$TestDirectory = '')
$ErrorActionPreference='Stop'
$taskRoot=Split-Path -Parent $PSScriptRoot
if($TestDirectory -eq ''){$TestDirectory=Join-Path $taskRoot ('.tools\windows-network-'+[Guid]::NewGuid().ToString('N'))}
$TestDirectory=[IO.Path]::GetFullPath($TestDirectory)
New-Item -ItemType Directory -Force $TestDirectory | Out-Null
$previous=$env:BLIZKO_SERVER_URL
$server=$null
try {
 $server=Start-Process -FilePath (Join-Path $taskRoot 'dist\Blizko.Server.exe') -ArgumentList @('-listen','127.0.0.1:3340') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $TestDirectory 'server.out') -RedirectStandardError (Join-Path $TestDirectory 'server.err')
 Start-Sleep -Milliseconds 500
 if($server.HasExited){throw 'Local server did not start'}
 $env:BLIZKO_SERVER_URL='http://127.0.0.1:3340'
 $test=Start-Process -FilePath (Join-Path $taskRoot 'dist\windows\Blizko.exe') -ArgumentList @('--network-test',('"'+$TestDirectory+'"')) -WindowStyle Hidden -PassThru -Wait
 Get-Content -LiteralPath (Join-Path $TestDirectory 'network-test.txt')
 if($test.ExitCode -ne 0){throw 'Windows process delivery test failed'}
} finally {
 $env:BLIZKO_SERVER_URL=$previous
 if($null -ne $server -and -not $server.HasExited){Stop-Process -Id $server.Id}
}
