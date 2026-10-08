@echo off
setlocal EnableExtensions DisableDelayedExpansion
title LumaTape VHS demo

rem The demo has its own config. It never kills an existing app or changes a display mode.
tasklist /FI "IMAGENAME eq lumatape.exe" /NH /FO CSV 2>nul | find /I "lumatape.exe" >nul
if not errorlevel 1 goto app_running
tasklist /FI "IMAGENAME eq lumatape-testcard.exe" /NH /FO CSV 2>nul | find /I "lumatape-testcard.exe" >nul
if not errorlevel 1 goto card_running

if not defined LOCALAPPDATA goto no_localappdata
if not exist "%~dp0lumatape.exe" goto missing_files
if not exist "%~dp0lumatape-testcard.exe" goto missing_files
if not exist "%~dp0lumatape_capture.dll" goto missing_files
if not exist "%~dp0glfw3.dll" goto missing_files
if not exist "%~dp0examples\vhs-visible.json" goto missing_files

set "DEMO_DIR=%LOCALAPPDATA%\LumaTape\Demo"
if not exist "%DEMO_DIR%" mkdir "%DEMO_DIR%"
if not exist "%DEMO_DIR%" goto config_failed
copy /Y "%~dp0examples\vhs-visible.json" "%DEMO_DIR%\config.json" >nul
if errorlevel 1 goto config_failed

echo Starting a visible VHS demo: Full compatibility, CPU transfer, up to 30 FPS.
echo Ctrl+Alt+F9 toggles the effect. Ctrl+Alt+F10 hides it immediately.
echo Keep the test card focused. Exit LumaTape through its tray menu.
echo Demo settings and log: "%DEMO_DIR%"
start "" /D "%~dp0" "%~dp0lumatape-testcard.exe" --width 960 --height 720 --color-field
if errorlevel 1 goto start_failed

rem Wait for the real target title before LumaTape resolves its HWND.
set /A attempts=0 >nul
:wait_for_card
tasklist /FI "IMAGENAME eq lumatape-testcard.exe" /FI "WINDOWTITLE eq LumaTape test card" /NH /FO CSV 2>nul | find /I "lumatape-testcard.exe" >nul
if not errorlevel 1 goto start_filter
set /A attempts+=1 >nul
if %attempts% GEQ 20 goto start_failed
timeout /T 1 /NOBREAK >nul
goto wait_for_card

:start_filter
start "" /D "%~dp0" "%~dp0lumatape.exe" --config "%DEMO_DIR%\config.json" --mode full --capture-transfer compatibility --window "LumaTape test card"
if errorlevel 1 goto start_failed
exit /B 0

:app_running
echo LumaTape is already running. Close it through the tray menu, then try again.
goto failed
:card_running
echo A LumaTape test card is already running. Close that window, then try again.
goto failed
:missing_files
echo Demo files are missing. Extract the complete Full package into one folder.
echo Required: lumatape.exe, lumatape-testcard.exe, lumatape_capture.dll,
echo glfw3.dll and examples\vhs-visible.json next to this launcher.
goto failed
:no_localappdata
echo Windows LOCALAPPDATA is unavailable. Run this launcher in your desktop session.
goto failed
:config_failed
echo Could not write the separate demo config in "%LOCALAPPDATA%\LumaTape\Demo".
goto failed
:start_failed
echo The demo could not start. Close any demo window and try again.
:failed
pause
exit /B 1
