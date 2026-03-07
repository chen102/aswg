$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$toolDir = Join-Path $scriptDir "drawio-export"

Push-Location $toolDir
try {
  npm install --silent
  npm run export -- $args
} finally {
  Pop-Location
}
