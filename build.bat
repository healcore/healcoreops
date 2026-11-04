@echo off
chcp 65001 >nul
rem Сборка HealCoreOps в Windows. Нужен только Go (https://go.dev/dl/), компилятор C не требуется.
setlocal
cd /d "%~dp0"
set VERSION=1.8.7
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

where go >nul 2>nul || (echo Нужен Go 1.22+: https://go.dev/dl/ & exit /b 1)
if not exist dist mkdir dist
if not exist cmd\setup\payload mkdir cmd\setup\payload

echo [1/4] Зависимости
go mod tidy || exit /b 1
echo [2/4] Ресурсы (иконка, манифест, версия)
set GOOS=
go run ./tools/genres -icon Icon.png -out rsrc_windows_amd64.syso -desc "HealCoreOps" -name HealCoreOps.exe -version %VERSION% || exit /b 1
go run ./tools/genres -icon Icon.png -out cmd/setup/rsrc_windows_amd64.syso -desc "HealCoreOps Setup" -name HealCoreOps-Setup.exe -version %VERSION% || exit /b 1
copy /y Icon.png cmd\setup\icon.png >nul
set GOOS=windows
echo [3/4] Приложение
go build -trimpath -ldflags "-s -w -H windowsgui" -o cmd/setup/payload/HealCoreOps.exe . || exit /b 1
echo [4/4] Установщик
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/HealCoreOps-Setup.exe ./cmd/setup || exit /b 1
echo.
echo Готово: dist\HealCoreOps-Setup.exe
endlocal
