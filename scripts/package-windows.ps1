param([switch]$Modern)
$ErrorActionPreference='Stop'
$taskRoot=Split-Path -Parent $PSScriptRoot
# Use the same exact allowlist as the verified updater. Ignore stale launchers,
# local reports and any other unrelated files in dist/windows.
$source=[IO.File]::ReadAllText((Join-Path $taskRoot 'windows\UpdatePackage.cs'))
$declaration=[regex]::Match($source,'string\[\] Files = \{([^}]+)\}')
if(-not $declaration.Success){throw 'Updater allowlist missing'}
$names=@([regex]::Matches($declaration.Groups[1].Value,'"([^"]+)"') | ForEach-Object {$_.Groups[1].Value})
if($names.Count -ne 12){throw 'Review changed updater allowlist before packaging'}
if($Modern){$names=@($names | Where-Object {$_ -notin @('blizko_iroh.dll','IROH_NOTICES.txt')})}
$paths=@($names | ForEach-Object {Join-Path "$taskRoot\dist\windows" $_})
foreach($file in $paths){if(-not(Test-Path -LiteralPath $file -PathType Leaf)){throw "Missing package file: $file"}}
$name=if($Modern){'Blizko-Windows-x64-modern.zip'}else{'Blizko-Windows-x64.zip'}
Compress-Archive -LiteralPath $paths -DestinationPath (Join-Path "$taskRoot\dist" $name) -Force
