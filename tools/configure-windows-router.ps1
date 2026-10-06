param(
    [string]$Router = '192.168.80.1',
    [int]$InterfaceIndex = 0,
    [string]$V2rayConfig = '',
    [string]$BackupDirectory = '',
    [string]$ResultPath = ''
)
trap {
    if ($ResultPath) { [ordered]@{ Success=$false; Error=$_.Exception.Message } | ConvertTo-Json | Set-Content $ResultPath -Encoding UTF8 }
    Write-Error $_
    exit 1
}
$ErrorActionPreference = 'Stop'
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) { throw 'Run this script from an administrator PowerShell session.' }
if (-not $BackupDirectory) { $BackupDirectory = Join-Path $env:LOCALAPPDATA ('hy2route\network-backup-' + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
if (Test-Path $BackupDirectory) { throw 'Backup directory already exists; choose a new directory.' }
$routerRoutes = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' | Where-Object NextHop -eq $Router)
$indices = @($routerRoutes.InterfaceIndex | Select-Object -Unique)
if ($InterfaceIndex -gt 0) { if ($InterfaceIndex -notin $indices) { throw 'Selected adapter does not use the router gateway.' }; $indices = @($InterfaceIndex); $routerRoutes = @($routerRoutes | Where-Object InterfaceIndex -eq $InterfaceIndex) }
if ($indices.Count -ne 1) { throw 'Expected exactly one adapter with the selected router as its default gateway.' }
$routerIndex = $indices[0]
$uplinks = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.InterfaceIndex -ne $routerIndex -and $_.InterfaceAlias -ne 'singbox_tun' } | Select-Object -ExpandProperty InterfaceIndex -Unique)
$interfaces = @(Get-NetIPInterface -AddressFamily IPv4 | Where-Object { $_.InterfaceIndex -eq $routerIndex -or $_.InterfaceIndex -in $uplinks })
$networkBackup = @($interfaces | ForEach-Object {
    [ordered]@{ InterfaceIndex=$_.InterfaceIndex; AutomaticMetric=[string]$_.AutomaticMetric; InterfaceMetric=$_.InterfaceMetric }
})
$dnsBackup = @(Get-DnsClientServerAddress -InterfaceIndex $routerIndex -AddressFamily IPv4).ServerAddresses
$systemProxy = Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
$activeTun = Get-NetAdapter -Name 'singbox_tun' -ErrorAction SilentlyContinue | Where-Object Status -eq 'Up'
if ($activeTun -and -not $V2rayConfig) { throw 'Specify the active v2rayN configuration before disabling its tunnel.' }
$processes = @()
$v2rayRaw = $null
if ($V2rayConfig) {
    if (-not (Test-Path $V2rayConfig -PathType Leaf)) { throw 'v2rayN configuration does not exist.' }
    $v2rayRaw = [IO.File]::ReadAllText($V2rayConfig)
    $parsed = $v2rayRaw | ConvertFrom-Json
    if (-not $parsed.TunModeItem) { throw 'Unsupported v2rayN configuration: TunModeItem missing.' }
    $v2rayRoot = Split-Path (Split-Path $V2rayConfig -Parent) -Parent
    $processes = @(Get-CimInstance Win32_Process | Where-Object {
        $_.Name -in @('v2rayN.exe','sing-box.exe') -and $_.ExecutablePath -and
        $_.ExecutablePath.StartsWith($v2rayRoot + '\', [StringComparison]::OrdinalIgnoreCase)
    })
    if ((Get-NetAdapter -Name 'singbox_tun' -ErrorAction SilentlyContinue | Where-Object Status -eq 'Up') -and -not ($processes | Where-Object Name -eq 'sing-box.exe')) {
        throw 'The active tunnel does not belong to the specified v2rayN directory.'
    }
}
New-Item -ItemType Directory -Path $BackupDirectory | Out-Null
[ordered]@{ Router=$Router; RouterInterface=$routerIndex; Interfaces=$networkBackup; DefaultRoutes=@($routerRoutes | Select-Object InterfaceIndex,DestinationPrefix,NextHop,RouteMetric); DnsServers=@($dnsBackup); ProxyEnable=$systemProxy.ProxyEnable; ProxyServer=$systemProxy.ProxyServer; V2rayConfig=$V2rayConfig } | ConvertTo-Json -Depth 10 | Set-Content (Join-Path $BackupDirectory 'network.json') -Encoding UTF8
if ($v2rayRaw) { [IO.File]::WriteAllText((Join-Path $BackupDirectory 'guiNConfig.json'),$v2rayRaw,[Text.UTF8Encoding]::new($false)) }
try {
    # Stop only the matching VPN application/core. Saved node definitions stay intact.
    foreach ($process in ($processes | Sort-Object @{Expression={ if ($_.Name -eq 'v2rayN.exe') {0} else {1} }})) {
        Stop-Process -Id $process.ProcessId -Force -ErrorAction SilentlyContinue
    }
    if ($v2rayRaw) {
        $matches = [regex]::Matches($v2rayRaw, '"EnableTun"\s*:\s*(true|false)')
        if ($matches.Count -ne 1) { throw 'Expected one EnableTun setting.' }
        $updated = [regex]::Replace($v2rayRaw, '("EnableTun"\s*:\s*)(true|false)', '${1}false')
        [IO.File]::WriteAllText($V2rayConfig,$updated,[Text.UTF8Encoding]::new($false))
    }
    if ($V2rayConfig) { Get-NetAdapter -Name 'singbox_tun' -ErrorAction SilentlyContinue | Disable-NetAdapter -Confirm:$false }
    # A low adapter metric plus a low default-route metric makes the router
    # preferred even when a previously configured Ethernet route had metric 100.
    Set-NetIPInterface -InterfaceIndex $routerIndex -AddressFamily IPv4 -AutomaticMetric Disabled -InterfaceMetric 10
    foreach ($index in $uplinks) { Set-NetIPInterface -InterfaceIndex $index -AddressFamily IPv4 -AutomaticMetric Disabled -InterfaceMetric 200 }
    foreach ($route in $routerRoutes) { $route | Set-NetRoute -RouteMetric 0 }
    Set-DnsClientServerAddress -InterfaceIndex $routerIndex -ServerAddresses $Router
    if ($systemProxy.ProxyServer -match '(127\.0\.0\.1|localhost)') {
        Set-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -Name ProxyEnable -Value 0
    }
    Clear-DnsClientCache
    $result = [ordered]@{ Success=$true; Router=$Router; RouterInterface=$routerIndex; Backup=$BackupDirectory; V2rayTunEnabled=$false; Routes=@(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' | Select-Object InterfaceIndex,NextHop,RouteMetric) }
    if ($ResultPath) { $result | ConvertTo-Json -Depth 10 | Set-Content $ResultPath -Encoding UTF8 }
    $result | ConvertTo-Json -Depth 10
} catch {
    $failureMessage = $_.Exception.Message
    # Restore changed settings if any step fails.
    foreach ($item in $networkBackup) {
        if ($item.AutomaticMetric -eq 'Enabled') { Set-NetIPInterface -InterfaceIndex $item.InterfaceIndex -AddressFamily IPv4 -AutomaticMetric Enabled -ErrorAction Continue }
        else { Set-NetIPInterface -InterfaceIndex $item.InterfaceIndex -AddressFamily IPv4 -AutomaticMetric Disabled -InterfaceMetric $item.InterfaceMetric -ErrorAction Continue }
    }
    foreach ($route in $routerRoutes) { $route | Set-NetRoute -RouteMetric $route.RouteMetric -ErrorAction Continue }
    if ($dnsBackup.Count) { Set-DnsClientServerAddress -InterfaceIndex $routerIndex -ServerAddresses $dnsBackup -ErrorAction Continue }
    if ($v2rayRaw) { [IO.File]::WriteAllText($V2rayConfig,$v2rayRaw,[Text.UTF8Encoding]::new($false)) }
    if ($null -ne $systemProxy.ProxyEnable) { Set-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -Name ProxyEnable -Value $systemProxy.ProxyEnable -ErrorAction Continue }
    if ($v2rayRaw -and ($processes | Where-Object Name -eq 'v2rayN.exe')) {
        Start-Process -FilePath (Join-Path $v2rayRoot 'v2rayN.exe') -ErrorAction Continue
    }
    if ($ResultPath) { [ordered]@{ Success=$false; Error=$failureMessage; Backup=$BackupDirectory } | ConvertTo-Json | Set-Content $ResultPath -Encoding UTF8 }
    throw
}
