. "$PSScriptRoot\env.ps1"
$taskStage = Join-Path $taskRoot ('.tools\source-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force $taskStage | Out-Null
# An explicit allowlist keeps SDKs, runtime state, signing keys and build caches out.
$taskItems = @('.github','.gitignore','settings.gradle','build.gradle','gradle.properties','README.md','THIRD_PARTY_NOTICES.txt','licenses','distribution','scripts','core','ios\Sources','ios\Info.plist','ios\project.yml','app\src','app\build.gradle')
foreach ($taskItem in $taskItems) {
    $taskFrom = Join-Path $taskRoot $taskItem
    $taskTo = Join-Path $taskStage $taskItem
    $taskParent = Split-Path -Parent $taskTo
    New-Item -ItemType Directory -Force $taskParent | Out-Null
    Copy-Item -LiteralPath $taskFrom -Destination $taskTo -Recurse -Exclude '__pycache__','*.pyc'
}
New-Item -ItemType Directory -Force "$taskRoot\dist" | Out-Null
$taskZip = "$taskRoot\dist\Blizko-source.zip"
if (Test-Path -LiteralPath $taskZip) { Remove-Item -LiteralPath $taskZip }
[IO.Compression.ZipFile]::CreateFromDirectory($taskStage,$taskZip)
$taskArchive = [IO.Compression.ZipFile]::OpenRead($taskZip)
try {
    if ($taskArchive.Entries.FullName -match '(^|/)(\.tools|node_modules|build|\.gradle)/|\.keystore$|\.jks$|local\.properties$') { throw 'Unexpected private/build file in archive' }
    "Source entries: $($taskArchive.Entries.Count)"
} finally { $taskArchive.Dispose() }
