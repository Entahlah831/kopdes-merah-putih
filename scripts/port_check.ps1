<#
.SYNOPSIS
Memeriksa konflik port internal & ketersediaan port host sebelum menjalankan Kopdes.
#>
param(
    [string]$EnvFile = ".env.test"
)
$ErrorActionPreference = "Stop"
$absEnv = [System.IO.Path]::GetFullPath($EnvFile)
go -C backend run ./cmd/envcheck --ports --strict --env-file $absEnv
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
