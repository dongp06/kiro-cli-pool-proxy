<#
  setup-client.ps1 — Zero-login client setup for Kiro CLI Pool Proxy (Windows).

  Máy khách KHÔNG cần login. Script:
    1) trỏ endpoint kiro-cli vào proxy (api.krs/cps/codewhisperer.service)
    2) seed 1 token "giả" vào auth_kv trong data.sqlite3 để qua cổng login local
  Proxy sẽ swap Authorization bằng account thật trong pool.

  Usage:
    ./setup-client.ps1 -Proxy http://PROXY_IP:9999 [-Region us-east-1] [-ApiKey kpp_xxx]
    ./setup-client.ps1 -Reset

  Yêu cầu: python (python/python3) HOẶC sqlite3.exe trong PATH để seed token.
#>
param(
    [string]$Proxy,
    [string]$Region = "us-east-1",
    [string]$ApiKey = "POOL_PLACEHOLDER",
    [switch]$Reset
)
$ErrorActionPreference = "Stop"

$Kiro = if ($env:KIRO_CLI) { $env:KIRO_CLI } else { "kiro-cli" }
if (-not (Get-Command $Kiro -ErrorAction SilentlyContinue)) {
    Write-Error "kiro-cli not found (set `$env:KIRO_CLI). "; exit 1
}

# data.sqlite3 location on Windows: %LOCALAPPDATA%\kiro-cli\data.sqlite3
$DataDir = if ($env:KIRO_DATA_DIR) { $env:KIRO_DATA_DIR } else { Join-Path $env:LOCALAPPDATA "kiro-cli" }
$DB = Join-Path $DataDir "data.sqlite3"

if ($Reset) {
    & $Kiro settings -d api.krs.service 2>$null
    & $Kiro settings -d api.cps.service 2>$null
    & $Kiro settings -d api.codewhisperer.service 2>$null
    Write-Host "✅ Endpoints reset. (Token giả trong auth_kv vẫn còn — chạy 'kiro-cli login' nếu muốn dùng account thật.)"
    exit 0
}

if (-not $Proxy) {
    Write-Host "Usage: ./setup-client.ps1 -Proxy http://PROXY_IP:9999 [-Region us-east-1] [-ApiKey kpp_xxx]"
    Write-Host "       ./setup-client.ps1 -Reset"
    exit 1
}

$Val = "{`"endpoint`":`"$Proxy`",`"region`":`"$Region`"}"

Write-Host "[1/3] Tro kiro-cli vao proxy: $Proxy (region=$Region)"
& $Kiro settings api.krs.service $Val
& $Kiro settings api.cps.service $Val
& $Kiro settings api.codewhisperer.service $Val

# Dam bao data.sqlite3 + migrations (bang auth_kv) ton tai.
& $Kiro settings api.krs.service *> $null
if (-not (Test-Path $DB)) {
    Write-Error "Khong tim thay $DB (kiro-cli chua khoi tao). Chay 'kiro-cli settings all' roi thu lai."
    exit 1
}

Write-Host "[2/3] Seed token vao auth_kv (bo qua login)"
$Token = @{
    access_token  = $ApiKey
    refresh_token = $ApiKey
    expires_at    = "2099-01-01T00:00:00Z"
    region        = $Region
    start_url     = "https://pool.local/start"
    scopes        = @("codewhisperer:completions","codewhisperer:analysis","codewhisperer:conversations")
    oauth_flow    = "DeviceCode"
} | ConvertTo-Json -Compress
$Reg = @{
    client_id                = "pool"
    client_secret            = "pool"
    client_secret_expires_at = "2099-01-01T00:00:00Z"
    region                   = $Region
    scopes                   = @("codewhisperer:completions","codewhisperer:analysis","codewhisperer:conversations")
    oauth_flow               = "DeviceCode"
} | ConvertTo-Json -Compress

function Find-Python {
    foreach ($p in @("python","python3","py")) {
        if (Get-Command $p -ErrorAction SilentlyContinue) { return $p }
    }
    return $null
}

$py = Find-Python
if ($py) {
    $script = @'
import sqlite3,sys
db,tok,reg=sys.argv[1],sys.argv[2],sys.argv[3]
c=sqlite3.connect(db)
c.execute("insert or replace into auth_kv(key,value) values(?,?)",("kirocli:odic:token",tok))
c.execute("insert or replace into auth_kv(key,value) values(?,?)",("kirocli:odic:device-registration",reg))
c.commit(); print("   seeded:",[r[0] for r in c.execute("select key from auth_kv")])
'@
    $tmp = Join-Path $env:TEMP "kpp_seed.py"
    Set-Content -Path $tmp -Value $script -Encoding UTF8
    & $py $tmp $DB $Token $Reg
    Remove-Item $tmp -ErrorAction SilentlyContinue
}
elseif (Get-Command sqlite3 -ErrorAction SilentlyContinue) {
    $t = $Token -replace "'","''"
    $r = $Reg   -replace "'","''"
    & sqlite3 $DB "INSERT OR REPLACE INTO auth_kv(key,value) VALUES('kirocli:odic:token','$t');"
    & sqlite3 $DB "INSERT OR REPLACE INTO auth_kv(key,value) VALUES('kirocli:odic:device-registration','$r');"
    Write-Host "   seeded via sqlite3"
}
else {
    Write-Error "Can python hoac sqlite3.exe trong PATH de seed token."
    exit 1
}

Write-Host "[3/3] Kiem tra proxy: $Proxy/health"
try {
    $resp = Invoke-WebRequest -Uri "$Proxy/health" -TimeoutSec 5 -UseBasicParsing
    Write-Host "   -> $($resp.Content)"
} catch {
    Write-Host "   (khong toi duoc proxy — kiem tra firewall/IP)"
}

Write-Host ""
Write-Host "✅ Xong. May khach KHONG can login. Chay:"
Write-Host "     kiro-cli chat"
Write-Host "   Khoi phuc: ./setup-client.ps1 -Reset"
