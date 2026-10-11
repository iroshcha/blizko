param(
    [ValidateSet('Login', 'Configure', 'Start', 'Check')]
    [string]$Mode = 'Start',
    [string]$Domain = '',
    [string]$TunaConfigPath = '',
    [ValidateRange(1, 600)]
    [int]$CheckSeconds = 1
)
$ErrorActionPreference = 'Stop'
$serverRoot = $PSScriptRoot
$settingsPath = Join-Path $serverRoot 'settings.json'
$tunaData = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'BlizkoRelay'
$tunaConfig = Join-Path $tunaData 'tuna.yml'
if ($TunaConfigPath -ne '') {
    if (-not [IO.Path]::IsPathRooted($TunaConfigPath)) { throw 'TunaConfigPath must be an absolute file path.' }
    $tunaConfig = [IO.Path]::GetFullPath($TunaConfigPath)
    $tunaData = [IO.Path]::GetDirectoryName($tunaConfig)
}

function Find-Tuna {
    $portable = Join-Path $serverRoot 'tuna.exe'
    if (Test-Path -LiteralPath $portable -PathType Leaf) { return $portable }
    $command = Get-Command tuna.exe -CommandType Application -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    foreach ($scope in @('User', 'Machine')) {
        foreach ($directory in ([Environment]::GetEnvironmentVariable('Path', $scope) -split ';')) {
            if ([string]::IsNullOrWhiteSpace($directory)) { continue }
            $candidate = Join-Path ([Environment]::ExpandEnvironmentVariables($directory.Trim('"'))) 'tuna.exe'
            if (Test-Path -LiteralPath $candidate -PathType Leaf) { return $candidate }
        }
    }
    throw 'Клиент Tuna не найден. Установите tuna_x64.msi по ссылке из README.txt и снова откройте этот файл.'
}

function ConvertTo-TunaHost([string]$value) {
    $value = $value.Trim().ToLowerInvariant()
    if ($value -notmatch '^https://') { $value = 'https://' + $value }
    $uri = $null
    if (-not [Uri]::TryCreate($value, [UriKind]::Absolute, [ref]$uri)) { throw 'Введите постоянный адрес из раздела «Домены» Tuna.' }
    if ($uri.Scheme -ne 'https' -or $uri.UserInfo -ne '' -or $uri.Query -ne '' -or $uri.Fragment -ne '' -or $uri.AbsolutePath -ne '/' -or -not $uri.IsDefaultPort) {
        throw 'Нужен только HTTPS-адрес без пароля, пути, параметров или номера порта.'
    }
    if ($uri.Host -notmatch '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.ru\.tuna\.am$') {
        throw 'Нужен адрес российского региона вида имя.ru.tuna.am. Скопируйте его в разделе «Домены» Tuna.'
    }
    return $uri.Host
}

try {
    if (-not [Environment]::Is64BitOperatingSystem) { throw 'Этот комплект предназначен для 64-разрядной Windows 10/11.' }
    switch ($Mode) {
        'Login' {
            $tuna = Find-Tuna
            New-Item -ItemType Directory -Force -Path $tunaData | Out-Null
            Write-Host 'Войдите в свой аккаунт Tuna. Если браузер не открылся, используйте ссылку, которую покажет клиент.'
            & $tuna login "--config=$tunaConfig"
            if ($LASTEXITCODE -ne 0) { throw 'Tuna не завершила вход. Повторите вход и проверьте подключение к интернету.' }
            Write-Host 'Вход завершён. Теперь откройте 2-Setup.cmd.'
            break
        }
        'Configure' {
            if ($Domain -eq '') { $Domain = Read-Host 'Вставьте свой БЕСПЛАТНЫЙ постоянный адрес Tuna (имя.ru.tuna.am)' }
            $hostname = ConvertTo-TunaHost $Domain
            $url = 'https://' + $hostname + '/'
            [IO.File]::WriteAllText($settingsPath, (@{ host = $hostname } | ConvertTo-Json), [Text.Encoding]::UTF8)
            [IO.File]::WriteAllText((Join-Path $serverRoot 'Server-address.txt'), $url + [Environment]::NewLine, [Text.Encoding]::UTF8)
            $launcher = @(
                '@echo off',
                'setlocal',
                'if not exist "%~dp0Blizko.exe" (',
                '  echo Copy this file into the folder containing Blizko.exe.',
                '  pause',
                '  exit /b 1',
                ')',
                ('set "BLIZKO_RELAY_URL=' + $url + '"'),
                'start "" "%~dp0Blizko.exe"'
            ) -join "`r`n"
            [IO.File]::WriteAllText((Join-Path $serverRoot 'Blizko-Home.cmd'), $launcher + "`r`n", [Text.Encoding]::ASCII)
            Write-Host "Сохранён адрес: $url"
            Write-Host 'Файл Blizko-Home.cmd создан. Скопируйте его в папку приложения на каждом Windows-компьютере.'
            Write-Host 'На серверном компьютере теперь откройте 3-Start-Server.cmd.'
            break
        }
        { $_ -in @('Start', 'Check') } {
            $tuna = $null
            $hostname = $null
            if ($Mode -eq 'Start') {
                $tuna = Find-Tuna
                if (-not (Test-Path -LiteralPath $settingsPath -PathType Leaf)) { throw 'Сначала откройте 2-Setup.cmd и сохраните свой бесплатный адрес Tuna.' }
                $hostname = ConvertTo-TunaHost ((Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json).host)
                if (-not (Test-Path -LiteralPath $tunaConfig -PathType Leaf)) { throw 'Сначала откройте 1-Login-Tuna.cmd и войдите в аккаунт.' }
            }
            $relayExe = Join-Path $serverRoot 'Blizko.Server.exe'
            if (-not (Test-Path -LiteralPath $relayExe)) { throw 'Распакуйте архив целиком. Не запускайте файлы прямо из ZIP.' }
            $listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 3340)
            try { $listener.Start() } catch { throw 'Порт 3340 уже занят. Закройте ранее запущенный сервер. Если его окно уже закрыто, перезапустите компьютер.' } finally { $listener.Stop() }
            if (-not ('BlizkoRelayProcessJob' -as [type])) { Add-Type -Path (Join-Path $serverRoot 'RelayProcessJob.cs') }
            $logs = Join-Path $serverRoot 'logs'
            New-Item -ItemType Directory -Force -Path $logs | Out-Null
            $job = New-Object BlizkoRelayProcessJob
            $serverArguments = @('-listen', '127.0.0.1:3340')
            if ($hostname) { $serverArguments += @('-web-origin', ('https://' + $hostname)) }
            $tokenFile = Join-Path $serverRoot 'diagnostics-private\access-code.txt'
            if (Test-Path -LiteralPath $tokenFile -PathType Leaf) { $serverArguments += @('-token', ('"' + $tokenFile + '"'), '-data', ('"' + (Join-Path $serverRoot 'diagnostics-private\journal') + '"')) }
            $relayProcess = $null
            $tunaProcess = $null
            try {
                $relayProcess = Start-Process -FilePath $relayExe -ArgumentList $serverArguments -WorkingDirectory $serverRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $logs 'server.out.log') -RedirectStandardError (Join-Path $logs 'server.err.log')
                $job.Add($relayProcess)
                $ready = $false
                for ($attempt = 0; $attempt -lt 20; $attempt++) {
                    if ($relayProcess.HasExited) { throw 'Сервер Близко остановился. Подробности в logs\server.err.log.' }
                    try {
                        $response = Invoke-WebRequest -Uri 'http://127.0.0.1:3340/healthz' -UseBasicParsing -TimeoutSec 1
                        if ($response.StatusCode -eq 204) { $ready = $true; break }
                    } catch { Start-Sleep -Milliseconds 200 }
                }
                if (-not $ready) { throw 'Локальный сервер не ответил. Подробности в logs\server.err.log.' }
                if ($Mode -eq 'Check') {
                    Write-Host 'PASS: локальный сервер Близко запущен и отвечает на 127.0.0.1:3340.'
                    Start-Sleep -Seconds $CheckSeconds
                    break
                }
                $tunaArguments = @('http', 'http://127.0.0.1:3340', ('--subdomain=' + $hostname.Replace('.ru.tuna.am', '')), '--location=ru', '--inspect=false', '--https-redirect', '--no-colors', ('--config="' + $tunaConfig + '"'))
                $tunaProcess = Start-Process -FilePath $tuna -ArgumentList $tunaArguments -WorkingDirectory $serverRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $logs 'tuna.out.log') -RedirectStandardError (Join-Path $logs 'tuna.err.log')
                $job.Add($tunaProcess)
                Write-Host "Адрес: https://$hostname/"
                Write-Host 'Локальный сервер работает, Tuna подключается. Это ещё не подтверждение доставки сообщений.'
                Write-Host 'Оставьте это окно открытым. Чтобы остановить сервер, нажмите Ctrl+C или закройте окно.'
                Write-Host 'Для проверки пришлите в чат только адрес выше. Пароль и токен не нужны.'
                Write-Host 'Журнал Tuna: logs\tuna.out.log и logs\tuna.err.log.'
                while (-not $relayProcess.HasExited -and -not $tunaProcess.HasExited) { Start-Sleep -Seconds 1 }
                if ($tunaProcess.HasExited) { throw 'Tuna остановилась. Откройте logs\tuna.out.log и logs\tuna.err.log: там указана причина.' }
                throw 'Сервер Близко остановился. Подробности в logs\server.err.log.'
            } finally {
                $job.Dispose()
                foreach ($child in @($tunaProcess, $relayProcess)) {
                    if ($null -ne $child -and -not $child.HasExited) { Stop-Process -Id $child.Id -ErrorAction SilentlyContinue }
                }
            }
            break
        }
    }
} catch {
    Write-Host ('Ошибка: ' + $_.Exception.Message) -ForegroundColor Red
    exit 1
}
