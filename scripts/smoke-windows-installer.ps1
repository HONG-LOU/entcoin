param(
    [Parameter(Mandatory = $true)]
    [string]$Installer
)

$ErrorActionPreference = "Stop"

function Assert-True([bool]$Condition, [string]$Message) {
    if (-not $Condition) {
        throw $Message
    }
}

function Wait-Until([scriptblock]$Condition, [string]$Message, [int]$Seconds = 20) {
    $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
    do {
        if (& $Condition) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw $Message
}

$installerPath = (Resolve-Path -LiteralPath $Installer).Path
$installDirectory = Join-Path $env:LOCALAPPDATA "Programs\Entcoin"
$executable = Join-Path $installDirectory "Entcoin.exe"
$uninstaller = Join-Path $installDirectory "uninstall.exe"
$protocolKey = "Registry::HKEY_CURRENT_USER\Software\Classes\entcoin"
$protocolCommandKey = Join-Path $protocolKey "shell\open\command"
$clientDirectory = Join-Path $env:APPDATA "Entropy\mainnet-v1"
$clientDatabase = Join-Path $clientDirectory "entpay-client.db"
$artifactDirectory = Join-Path $env:USERPROFILE "Downloads\EntPay"
$artifact = Join-Path $artifactDirectory "installer-preservation.txt"
$uri = "entcoin://pay?v=1&merchant=https%3A%2F%2Fmerchant.example%2Fentpay%2F"

New-Item -ItemType Directory -Force -Path $clientDirectory, $artifactDirectory | Out-Null
[IO.File]::WriteAllText($clientDatabase, "preserve-client-database", [Text.Encoding]::UTF8)
[IO.File]::WriteAllText($artifact, "preserve-verified-artifact", [Text.Encoding]::UTF8)
$clientHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $clientDatabase).Hash
$artifactHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $artifact).Hash

$install = Start-Process -FilePath $installerPath -ArgumentList "/S" -Wait -PassThru
Assert-True ($install.ExitCode -eq 0) "NSIS installer failed with exit code $($install.ExitCode)"
Assert-True (Test-Path -LiteralPath $executable -PathType Leaf) "installed Entcoin.exe is missing"
Assert-True (Test-Path -LiteralPath $uninstaller -PathType Leaf) "installed uninstaller is missing"
Assert-True (Test-Path -LiteralPath $protocolCommandKey) "entcoin protocol command was not registered"

$registeredCommand = (Get-Item -LiteralPath $protocolCommandKey).GetValue("")
$expectedCommand = '"' + $executable + '" "%1"'
Assert-True ($registeredCommand -ieq $expectedCommand) "protocol command is not exactly quoted: $registeredCommand"
Assert-True (@(Get-CimInstance Win32_Process -Filter "Name = 'Entcoin.exe'" |
    Where-Object { $_.ExecutablePath -eq $executable }).Count -eq 0) "Entcoin was already running before the protocol cold start"

Start-Process $uri
Wait-Until {
    @(Get-CimInstance Win32_Process -Filter "Name = 'Entcoin.exe'" |
        Where-Object { $_.ExecutablePath -eq $executable }).Count -eq 1
} "protocol launch did not start exactly one installed Entcoin process"

$processes = @(Get-CimInstance Win32_Process -Filter "Name = 'Entcoin.exe'" |
    Where-Object { $_.ExecutablePath -eq $executable })
$desktopProcess = $processes[0]
Assert-True ($desktopProcess.CommandLine -notmatch "(?i)([?&]handoff=|claim_token)") "a handoff capability entered the process command line"

Wait-Until {
    @(Get-NetTCPConnection -State Listen -LocalAddress "127.0.0.1" -LocalPort 47833 -ErrorAction SilentlyContinue |
        Where-Object { $_.OwningProcess -eq $desktopProcess.ProcessId }).Count -eq 1
} "installed Entcoin did not bind the handoff relay to 127.0.0.1:47833"

Start-Process $uri
Start-Sleep -Seconds 2
$processes = @(Get-CimInstance Win32_Process -Filter "Name = 'Entcoin.exe'" |
    Where-Object { $_.ExecutablePath -eq $executable })
Assert-True ($processes.Count -eq 1) "second protocol launch created more than one Entcoin process"

$random = [byte[]]::new(32)
[Security.Cryptography.RandomNumberGenerator]::Fill($random)
$handoff = [Convert]::ToBase64String($random).TrimEnd("=").Replace("+", "-").Replace("/", "_")
$body = @{ merchant = "https://merchant.example/entpay/"; handoff = $handoff } | ConvertTo-Json -Compress
$headers = @{ Origin = "https://merchant.example" }

$wrongOriginStatus = 0
try {
    Invoke-WebRequest -Uri "http://127.0.0.1:47833/v1/handoffs" -Method Post -ContentType "application/json" -Headers @{ Origin = "https://attacker.example" } -Body $body | Out-Null
} catch {
    $wrongOriginStatus = [int]$_.Exception.Response.StatusCode
}
Assert-True ($wrongOriginStatus -eq 403) "wrong Origin returned $wrongOriginStatus instead of 403"

$accepted = Invoke-WebRequest -Uri "http://127.0.0.1:47833/v1/handoffs" -Method Post -ContentType "application/json" -Headers $headers -Body $body
Assert-True ($accepted.StatusCode -eq 202) "authorized handoff returned $($accepted.StatusCode) instead of 202"

$replayStatus = 0
try {
    Invoke-WebRequest -Uri "http://127.0.0.1:47833/v1/handoffs" -Method Post -ContentType "application/json" -Headers $headers -Body $body | Out-Null
} catch {
    $replayStatus = [int]$_.Exception.Response.StatusCode
}
Assert-True ($replayStatus -eq 403) "replayed handoff returned $replayStatus instead of 403"

Stop-Process -Id $desktopProcess.ProcessId -Force
Wait-Until {
    @(Get-CimInstance Win32_Process -Filter "Name = 'Entcoin.exe'" |
        Where-Object { $_.ExecutablePath -eq $executable }).Count -eq 0
} "installed Entcoin process did not stop"

$uninstall = Start-Process -FilePath $uninstaller -ArgumentList "/S" -Wait -PassThru
Assert-True ($uninstall.ExitCode -eq 0) "NSIS uninstaller failed with exit code $($uninstall.ExitCode)"
Wait-Until { -not (Test-Path -LiteralPath $executable) } "Entcoin.exe remained after uninstall"
Assert-True (-not (Test-Path -LiteralPath $protocolKey)) "owned entcoin protocol key remained after uninstall"
Assert-True ((Get-FileHash -Algorithm SHA256 -LiteralPath $clientDatabase).Hash -eq $clientHash) "uninstall changed or removed entpay-client.db"
Assert-True ((Get-FileHash -Algorithm SHA256 -LiteralPath $artifact).Hash -eq $artifactHash) "uninstall changed or removed the delivered artifact"

Write-Host "Windows installer smoke test passed: registration, launch, single instance, relay, replay, and uninstall preservation."
