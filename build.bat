@echo off
setlocal enabledelayedexpansion

echo ===================================================
echo           SubtitleThing Build Automator
echo ===================================================
echo.

:: Discover directory containing this batch script
set "REPO_ROOT=%~dp0"
cd /d "%REPO_ROOT%SubtitleThing"

if not exist wails.json (
    echo [ERROR] Wails configuration wails.json not found!
    echo Ensure this script is placed in the repository root directory.
    pause
    exit /b 1
)

echo [*] Triggering Wails production compiler...
wails build

if %ERRORLEVEL% equ 0 (
    echo.
    echo ===================================================
    echo [+] Build Completed Successfully!
    echo [+] Target binary: SubtitleThing\build\bin\SubtitleThing.exe
    echo ===================================================
    echo.
) else (
    echo.
    echo ===================================================
    echo [ERROR] Build Failed! Check compilation logs above.
    echo ===================================================
    echo.
    pause
    exit /b %ERRORLEVEL%
)

endlocal
