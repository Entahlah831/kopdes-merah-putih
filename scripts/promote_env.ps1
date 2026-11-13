<#
.SYNOPSIS
Menyiapkan atau memperbarui berkas .env target dari .env.example tanpa menimpa rahasia yang sudah ada.
#>
param(
    [string]$Template = ".env.example",
    [string]$Target = ".env",
    [switch]$GenerateEmptySecrets
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $Template)) {
    throw "Template $Template tidak ditemukan."
}

$existing = @{}
if (Test-Path $Target) {
    foreach ($line in Get-Content $Target) {
        $trim = $line.Trim()
        if ($trim -eq "" -or $trim.StartsWith("#")) { continue }
        $idx = $trim.IndexOf("=")
        if ($idx -gt 0) {
            $k = $trim.Substring(0, $idx).Trim()
            $v = $trim.Substring($idx + 1).Trim()
            $existing[$k] = $v
        }
    }
}

$secretKeys = @(
    "JWT_SECRET", "AES_MASTER_KEY", "BLIND_INDEX_SECRET", "QRIS_SECRET",
    "METRICS_TOKEN", "DB_PASSWORD", "POSTGRES_PASSWORD", "KOPDES_APP_PASSWORD",
    "BACKUP_ENCRYPTION_KEY"
)

function New-RandomHex([int]$Bytes = 32) {
    $buf = New-Object byte[] $Bytes
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($buf)
    return ($buf | ForEach-Object { $_.ToString("x2") }) -join ""
}

$outLines = New-Object System.Collections.Generic.List[string]
foreach ($line in Get-Content $Template) {
    $trim = $line.Trim()
    if ($trim -eq "" -or $trim.StartsWith("#")) {
        $outLines.Add($line)
        continue
    }
    $idx = $line.IndexOf("=")
    if ($idx -le 0) {
        $outLines.Add($line)
        continue
    }
    $key = $line.Substring(0, $idx).Trim()
    $tplVal = $line.Substring($idx + 1).Trim()

    if ($existing.ContainsKey($key) -and $existing[$key] -ne "") {
        $outLines.Add("$key=$($existing[$key])")
    } elseif ($GenerateEmptySecrets -and ($secretKeys -contains $key) -and $tplVal -eq "") {
        $outLines.Add("$key=$(New-RandomHex 32)")
    } else {
        $outLines.Add("$key=$tplVal")
    }
}

$outLines | Set-Content -Path $Target -Encoding UTF8
Write-Host "Berkas $Target diperbarui dari $Template (nilai eksisting dipertahankan; rahasia tidak dicetak ke konsol)."
if (Get-Command go -ErrorAction SilentlyContinue) {
    go run ./cmd/envcheck --compare $Template $Target
}
