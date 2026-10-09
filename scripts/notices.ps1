. "$PSScriptRoot\env.ps1"
Push-Location "$taskRoot\core"
try {
    $taskModules = & go list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' all
    $taskText = [System.Text.StringBuilder]::new()
    [void]$taskText.AppendLine('Blizko: third-party software notices. Generated from pinned Go modules; some modules are build/test-only. Rust notices are in IROH_NOTICES.txt.')
    foreach ($taskLine in $taskModules) {
        $taskParts = $taskLine.Split('|')
        if ($taskParts.Length -lt 3 -or -not $taskParts[2]) { continue }
        $taskLicenses = Get-ChildItem -LiteralPath $taskParts[2] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)(\.|$)' }
        foreach ($taskLicense in $taskLicenses) {
            [void]$taskText.AppendLine("`n--- $($taskParts[0]) $($taskParts[1]) / $($taskLicense.Name) ---`n")
            [void]$taskText.AppendLine([IO.File]::ReadAllText($taskLicense.FullName))
        }
    }
    [void]$taskText.AppendLine("`n--- Go runtime ---`n")
    [void]$taskText.AppendLine([IO.File]::ReadAllText("$taskRoot\.tools\go\LICENSE"))
    [void]$taskText.AppendLine("`n--- Android QR dependencies ---`n")
    [void]$taskText.AppendLine([IO.File]::ReadAllText("$taskRoot\licenses\android-qr.txt"))
    [IO.File]::WriteAllText("$taskRoot\THIRD_PARTY_NOTICES.txt", $taskText.ToString())
    New-Item -ItemType Directory -Force "$taskRoot\app\src\main\assets", "$taskRoot\ios\Sources\Resources" | Out-Null
    Copy-Item "$taskRoot\THIRD_PARTY_NOTICES.txt" "$taskRoot\app\src\main\assets\THIRD_PARTY_NOTICES.txt"
    Copy-Item "$taskRoot\THIRD_PARTY_NOTICES.txt" "$taskRoot\ios\Sources\Resources\THIRD_PARTY_NOTICES.txt"
} finally { Pop-Location }
