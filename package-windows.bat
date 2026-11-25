@echo off
setlocal
pushd "%~dp0"

call build-windows.bat
if errorlevel 1 (
  popd
  exit /b 1
)

set "PKG_NAME=anki-passive-widget-windows-amd64"
set "STAGE_DIR=dist\%PKG_NAME%"
set "ZIP_PATH=dist\%PKG_NAME%.zip"

if exist "%STAGE_DIR%" rmdir /s /q "%STAGE_DIR%"
if exist "%ZIP_PATH%" del /f /q "%ZIP_PATH%"
mkdir "%STAGE_DIR%"

copy /y "anki-passive-widget.exe" "%STAGE_DIR%\" >nul
copy /y "README.md" "%STAGE_DIR%\" >nul

powershell -NoProfile -Command "Compress-Archive -Path '%STAGE_DIR%\*' -DestinationPath '%ZIP_PATH%' -Force"
if errorlevel 1 (
  echo Failed to create zip package
  popd
  exit /b 1
)

echo Package created: %ZIP_PATH%
popd
