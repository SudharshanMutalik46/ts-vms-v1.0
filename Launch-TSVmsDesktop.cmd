@echo off
setlocal
set "APP_DIR=%~dp0desktop\TSVmsDesktop\bin\Debug\net8.0-windows"
set "APP_EXE=%APP_DIR%\TSVmsDesktop.exe"

if not exist "%APP_EXE%" (
  echo Updated desktop client not found:
  echo %APP_EXE%
  exit /b 1
)

start "" "%APP_EXE%"
exit /b 0
