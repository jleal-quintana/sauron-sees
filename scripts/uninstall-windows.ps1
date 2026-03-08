param(
  [switch]$PurgeTemp,
  [string]$InstallRoot = "$env:LOCALAPPDATA\SauronSees",
  [string]$ConfigPath = "$env:APPDATA\SauronSees\config.toml"
)

$ErrorActionPreference = "Stop"

if ($env:OS -ne "Windows_NT") {
  throw "This uninstall script must run on Windows."
}

$exePath = Join-Path $InstallRoot "bin\sauron-sees.exe"

Get-Process sauron-sees -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

if (Test-Path $exePath) {
  & $exePath --config $ConfigPath uninstall-startup
  Remove-Item $exePath -Force
}

if ($PurgeTemp -and (Test-Path $ConfigPath)) {
  $configContent = Get-Content $ConfigPath -Raw
  if ($configContent -match 'temp_root\s*=\s*"(.+)"') {
    $tempRoot = $Matches[1] -replace '%USERPROFILE%', $env:USERPROFILE -replace '%LOCALAPPDATA%', $env:LOCALAPPDATA
    if (Test-Path $tempRoot) {
      Remove-Item $tempRoot -Recurse -Force
    }
  }
}

Write-Host "Uninstalled Sauron Sees startup and binary. Config and markdown files were preserved."
