param([string]$ServerRoot = $PSScriptRoot, [string]$WebOrigin = '')
$ErrorActionPreference = 'Stop'
# Run under the Windows account running the server, with the binary beside this script.
$root = [IO.Path]::GetFullPath($ServerRoot)
$binary = Join-Path $PSScriptRoot 'Blizko.Server.exe'
if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw 'Build/copy Blizko.Server.exe beside this script first.' }
$private = Join-Path $root 'diagnostics-private'
if (Test-Path -LiteralPath $private) {
    if ((Get-Item -LiteralPath $private).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Private directory must not be a link.' }
} else { New-Item -ItemType Directory -Path $private | Out-Null }
$acl = New-Object Security.AccessControl.DirectorySecurity
$acl.SetAccessRuleProtection($true, $false)
$inherit = [Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
foreach ($sid in @([Security.Principal.WindowsIdentity]::GetCurrent().User, (New-Object Security.Principal.SecurityIdentifier('S-1-5-18')))) {
    $rule = New-Object Security.AccessControl.FileSystemAccessRule($sid, 'FullControl', $inherit, 'None', 'Allow')
    $acl.AddAccessRule($rule)
}
Set-Acl -LiteralPath $private -AclObject $acl
$token = Join-Path $private 'access-code.txt'
if (-not (Test-Path -LiteralPath $token)) {
    & $binary -init -token $token
    if ($LASTEXITCODE -ne 0) { throw 'Cannot create diagnostic access code.' }
}
$arguments = @('-listen', '127.0.0.1:3340', '-data', (Join-Path $private 'journal'), '-token', $token)
if ($WebOrigin) { $arguments += @('-web-origin', $WebOrigin) }
& $binary @arguments
if ($LASTEXITCODE -ne 0) { throw 'Blizko server stopped.' }
