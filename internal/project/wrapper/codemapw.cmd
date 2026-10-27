@echo off
rem codemapw: runs the codemap release of codemap-wrapper.properties, downloaded once into
rem %CODEMAP_HOME% (default %LOCALAPPDATA%\codemap) and checked against the sha256 there. Written by
rem `codemap wrapper`; run it again to change the release.
setlocal
set "CODEMAPW_DIR=%~dp0"
set "CODEMAP_BIN="
for /f "delims=" %%i in ('powershell -NoProfile -ExecutionPolicy Bypass -Command "$ErrorActionPreference = 'Stop'; $p = Get-Content -Raw (Join-Path $env:CODEMAPW_DIR 'codemap-wrapper.properties') | ConvertFrom-StringData; $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }; $home_ = if ($env:CODEMAP_HOME) { $env:CODEMAP_HOME } else { Join-Path $env:LOCALAPPDATA 'codemap' }; $dir = Join-Path $home_ $p.version; $bin = Join-Path $dir 'codemap.exe'; if (-not (Test-Path $bin)) { $name = 'codemap_' + $p.version + '_windows_' + $arch + '.zip'; New-Item -ItemType Directory -Force $dir | Out-Null; $tmp = Join-Path $dir ('.download.' + [guid]::NewGuid()); New-Item -ItemType Directory $tmp | Out-Null; try { $zip = Join-Path $tmp $name; [Console]::Error.WriteLine('codemapw: downloading ' + $p.distribution_url + '/' + $name); [Net.ServicePointManager]::SecurityProtocol = 'Tls12'; $ProgressPreference = 'SilentlyContinue'; Invoke-WebRequest -UseBasicParsing ($p.distribution_url + '/' + $name) -OutFile $zip; if ((Get-FileHash -Algorithm SHA256 $zip).Hash -ne $p['sha256_windows_' + $arch]) { throw ('checksum of ' + $name + ' does not match codemap-wrapper.properties') }; Expand-Archive $zip $tmp; Move-Item -Force (Join-Path $tmp 'codemap.exe') $bin } finally { Remove-Item -Recurse -Force $tmp } }; $bin"') do set "CODEMAP_BIN=%%i"
if not defined CODEMAP_BIN exit /b 1
"%CODEMAP_BIN%" %*
