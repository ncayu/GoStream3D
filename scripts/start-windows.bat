@echo off
setlocal
title GoStream3D Launcher
if not exist "%~dp0start-windows.ps1" (
    echo Missing start-windows.ps1. Please extract the complete installation package.
    pause
    exit /b 1
)
"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-windows.ps1" %*
if errorlevel 1 (
    pause
    exit /b 1
)
exit /b 0
