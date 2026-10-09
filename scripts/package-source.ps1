. "$PSScriptRoot\env.ps1"
# Only committed source belongs in a release, never local credentials or native build outputs.
New-Item -ItemType Directory -Force "$taskRoot\dist" | Out-Null
$taskZip = "$taskRoot\dist\Blizko-source.zip"
& git -C $taskRoot archive --format=zip "--output=$taskZip" HEAD
if ($LASTEXITCODE -ne 0) { throw 'Source archive failed' }
$taskArchive = [IO.Compression.ZipFile]::OpenRead($taskZip)
try {
    if ($taskArchive.Entries.FullName -match '(^|/)(\.tools|node_modules|build|\.gradle)/|\.keystore$|\.jks$|local\.properties$') { throw 'Unexpected private/build file in archive' }
    "Source entries: $($taskArchive.Entries.Count)"
} finally { $taskArchive.Dispose() }
