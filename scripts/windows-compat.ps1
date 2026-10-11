param([Parameter(Mandatory=$true)][string]$OutputDirectory)
$ErrorActionPreference='Stop'
# Windows 0.2.5 only accepts this exact 12-file archive. Keep its genuine, UNUSED
# legacy DLL and license for one transition release. Neither new executable loads it.
$taskRoot=Split-Path -Parent $PSScriptRoot
$cache=Join-Path $taskRoot '.tools\windows-compat'
New-Item -ItemType Directory -Force $cache | Out-Null
$zip=Join-Path $cache 'v0.2.5.zip'
if (-not (Test-Path -LiteralPath $zip)) { Invoke-WebRequest 'https://github.com/iroshcha/blizko/releases/download/v0.2.5-windows/Blizko-Windows-x64.zip' -OutFile $zip }
if ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant() -ne '47e647c06e274b02bc06b0a9f8defbccfe3c69d60345b081411a2c91843d6bd5') { throw 'Legacy compatibility archive hash mismatch' }
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive=[IO.Compression.ZipFile]::OpenRead($zip)
try {
 foreach($name in @('blizko_iroh.dll','IROH_NOTICES.txt')) {
  $entry=$archive.GetEntry($name)
  if($null -eq $entry){throw 'Legacy compatibility file missing'}
  [IO.Compression.ZipFileExtensions]::ExtractToFile($entry,(Join-Path $OutputDirectory $name),$true)
 }
} finally {$archive.Dispose()}
