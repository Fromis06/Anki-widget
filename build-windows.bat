@echo off
setlocal
pushd "%~dp0"

taskkill /F /IM anki-passive-widget.exe /T >nul 2>&1

rsrc -ico build\windows\icon.ico -o rsrc.syso
if errorlevel 1 (
  echo Failed to generate icon resource
  popd
  exit /b 1
)

go build -ldflags "-w -s -H windowsgui" -o anki-passive-widget.exe .
if errorlevel 1 (
  popd
  exit /b 1
)

echo Build succeeded: anki-passive-widget.exe
