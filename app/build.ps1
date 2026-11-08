# Builds dist\CompaneroWoW.exe (Windows GUI executable, no CGO).
# Usage: powershell -File app\build.ps1
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

$goExe = 'go'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $fallback = 'C:\Program Files\Go\bin\go.exe'
    if (-not (Test-Path -LiteralPath $fallback)) {
        throw 'No encuentro go. Instala Go 1.27 o añádelo al PATH.'
    }
    $goExe = $fallback
}

$dist = Join-Path $PSScriptRoot '..\dist'
New-Item -ItemType Directory -Force -Path $dist | Out-Null

$env:CGO_ENABLED = '0'
& $goExe build -trimpath -ldflags '-s -w -H windowsgui' -o '..\dist\CompaneroWoW.exe' '.\cmd\companero'
if ($LASTEXITCODE -ne 0) {
    throw "go build terminó con código $LASTEXITCODE"
}

$exe = Get-Item -LiteralPath (Join-Path $dist 'CompaneroWoW.exe')
'{0} ({1:N2} MB)' -f $exe.FullName, ($exe.Length / 1MB)
