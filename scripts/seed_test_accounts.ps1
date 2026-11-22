<#
.SYNOPSIS
Mengisi akun dan data uji sintetis (Koperasi A & B) ke database *_test via cmd/seed_test_accounts.
#>
param(
    [string]$EnvFile = ".env.test",
    [string]$Password = ""
)

$ErrorActionPreference = "Stop"

$baseName = [System.IO.Path]::GetFileName($EnvFile)
if ($baseName -in @(".env", ".env.local", ".env.production", ".env.live")) {
    throw "DILARANG: seed_test_accounts tidak boleh dijalankan dengan berkas konfigurasi produksi/asli ($baseName)."
}

$absEnv = [System.IO.Path]::GetFullPath($EnvFile)
$argsList = @("-C", "backend", "run", "./cmd/seed_test_accounts", "--env-file", $absEnv)
if ($Password -ne "") {
    $argsList += @("--password", $Password)
}

& go @argsList
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
