<#
.SYNOPSIS
Memeriksa konflik port internal & ketersediaan port host sebelum menjalankan Kopdes.
#>
param(
    [string]$EnvFile = ".env.test"
)
$ErrorActionPreference = "Stop"
go run ./cmd/envcheck --ports --strict --env-file $EnvFile
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
