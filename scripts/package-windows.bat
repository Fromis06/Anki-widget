@echo off
setlocal
pushd "%~dp0\.."

call "%~dp0build-windows.bat"
if %errorlevel% neq 0 (
    popd
    exit /b %errorlevel%
)

if not exist dist mkdir dist
set ARCHIVE=dist\anki-passive-widget-windows-amd64.zip
if exist "%ARCHIVE%" del /q "%ARCHIVE%"

echo Creating %ARCHIVE%...
powershell -NoProfile -Command "Compress-Archive -Path 'anki-passive-widget.exe','README.md' -DestinationPath '%ARCHIVE%' -Force"
set PKG_ERR=%errorlevel%
popd
exit /b %PKG_ERR%
