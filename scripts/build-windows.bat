@echo off
setlocal
pushd "%~dp0\.."

where rsrc >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] rsrc tool not found in PATH. Install with:
    echo   go install github.com/akavel/rsrc@latest
    popd
    exit /b 1
)

echo Generating Windows icon resource...
rsrc -ico build\windows\icon.ico -o cmd\widget\rsrc.syso
if %errorlevel% neq 0 (
    popd
    exit /b %errorlevel%
)

echo Building anki-passive-widget.exe...
go build -ldflags "-w -s -H windowsgui" -o anki-passive-widget.exe ./cmd/widget
set BUILD_ERR=%errorlevel%
popd
exit /b %BUILD_ERR%
