param(
 [Parameter(Mandatory=$true)][string]$Archive,
 [Parameter(Mandatory=$true)][string]$Version,
 [Parameter(Mandatory=$true)][string]$Url,
 [Parameter(Mandatory=$true)][string]$SigningKey,
 [Parameter(Mandatory=$true)][string]$Output,
 [string]$Notes=''
)
$ErrorActionPreference='Stop'
Add-Type -AssemblyName System.Security
$taskRoot=Split-Path -Parent $PSScriptRoot
$releaseVersion=[Version]::Parse($Version)
if($releaseVersion.Revision -lt 0 -or $Url -notmatch '^https://github\.com/iroshcha/blizko/releases/download/[A-Za-z0-9._-]+/Blizko-Windows-x64\.zip$'){throw 'Invalid Windows release version or URL'}
$file=Get-Item -LiteralPath $Archive
if($file.Length -lt 1 -or $file.Length -gt 157286400){throw 'Invalid Windows archive size'}
$payload=@{version=$Version;url=$Url;size=$file.Length;sha256=(Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant();notes=$Notes}|ConvertTo-Json -Compress
$bytes=[Text.Encoding]::UTF8.GetBytes($payload)
$secret=[Security.Cryptography.ProtectedData]::Unprotect([IO.File]::ReadAllBytes($SigningKey),[Text.Encoding]::UTF8.GetBytes('blizko/windows-update-signing/1'),[Security.Cryptography.DataProtectionScope]::CurrentUser)
$rsa=New-Object Security.Cryptography.RSACryptoServiceProvider
$rsa.PersistKeyInCsp=$false
try{
 $rsa.FromXmlString([Text.Encoding]::UTF8.GetString($secret))
 $source=[IO.File]::ReadAllText((Join-Path $taskRoot 'windows\UpdateKey.cs'))
 if(-not $source.Contains($rsa.ToXmlString($false))){throw 'Signing key does not match pinned client key'}
 $signature=$rsa.SignData($bytes,[Security.Cryptography.CryptoConfig]::MapNameToOID('SHA256'))
 if(-not $rsa.VerifyData($bytes,[Security.Cryptography.CryptoConfig]::MapNameToOID('SHA256'),$signature)){throw 'Signature verification failed'}
 $envelope=@{payload=[Convert]::ToBase64String($bytes);signature=[Convert]::ToBase64String($signature)}|ConvertTo-Json
 [IO.File]::WriteAllText($Output,$envelope,(New-Object Text.UTF8Encoding($false)))
 Write-Output "Signed Windows update manifest: $Output"
}finally{[Array]::Clear($secret,0,$secret.Length);$rsa.Dispose()}
