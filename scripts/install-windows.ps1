param(
  [switch]$StartAgent,
  [string]$InstallRoot = "$env:LOCALAPPDATA\SauronSees",
  [string]$ConfigPath = "$env:APPDATA\SauronSees\config.toml"
)

$ErrorActionPreference = "Stop"

if ($env:OS -ne "Windows_NT") {
  throw "This installer must run on Windows."
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$binDir = Join-Path $InstallRoot "bin"
$exePath = Join-Path $binDir "sauron-sees.exe"
$codexConfigPath = Join-Path $HOME ".codex\config.toml"
$managedStart = "# BEGIN SAURON-SEES MANAGED PROFILE"
$managedEnd = "# END SAURON-SEES MANAGED PROFILE"
$managedBlock = @"
$managedStart
[profiles.sauron-sees-eod]
model = "gpt-5.4"
model_reasoning_effort = "medium"
sandbox_mode = "read-only"
approval_policy = "never"
$managedEnd
"@

New-Item -ItemType Directory -Force -Path $binDir | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $ConfigPath) | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $codexConfigPath) | Out-Null

if (Test-Path (Join-Path $repoRoot "go.mod")) {
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go is required to build from source."
  }
  Push-Location $repoRoot
  go build -o $exePath .\cmd\sauron-sees
  Pop-Location
} elseif (-not (Test-Path $exePath)) {
  throw "No source tree or prebuilt executable found."
}

if (-not (Test-Path $ConfigPath)) {
  Copy-Item (Join-Path $repoRoot "config.example.toml") $ConfigPath
}

$configContent = Get-Content $ConfigPath -Raw
foreach ($key in @("daily_markdown_root", "weekly_markdown_root", "temp_root")) {
  if ($configContent -match "$key\s*=\s*""(.+)""") {
    $resolved = $Matches[1] -replace '%USERPROFILE%', $env:USERPROFILE -replace '%LOCALAPPDATA%', $env:LOCALAPPDATA
    New-Item -ItemType Directory -Force -Path $resolved | Out-Null
  }
}

$existingCodex = ""
if (Test-Path $codexConfigPath) {
  $existingCodex = Get-Content $codexConfigPath -Raw
}
$pattern = [regex]::Escape($managedStart) + '.*?' + [regex]::Escape($managedEnd)
if ($existingCodex -match $pattern) {
  $updated = [regex]::Replace($existingCodex, $pattern, [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $managedBlock }, "Singleline")
  Set-Content -Path $codexConfigPath -Value $updated
} else {
  $separator = ""
  if ($existingCodex -and -not $existingCodex.EndsWith("`n")) { $separator = "`n" }
  Set-Content -Path $codexConfigPath -Value ($existingCodex + $separator + $managedBlock)
}

& $exePath --config $ConfigPath install-startup
& $exePath --config $ConfigPath doctor

if ($StartAgent) {
  Start-Process -FilePath $exePath -ArgumentList @("--config", $ConfigPath, "agent")
}

Write-Host "Installed Sauron Sees to $exePath"
