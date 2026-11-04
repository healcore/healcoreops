#!/usr/bin/env sh
# Сборка HealCoreOps. Компилятор C не нужен — всё на чистом Go,
# поэтому Windows-установщик собирается из любой ОС (Linux, macOS, Windows/Git Bash).
#
#   ./build.sh          → dist/HealCoreOps-Setup.exe + dist/healcoreops-linux-amd64
set -e
cd "$(dirname "$0")"
VERSION=1.8.1
export CGO_ENABLED=0

command -v go >/dev/null || { echo "Нужен Go 1.22+: https://go.dev/dl/"; exit 1; }
mkdir -p dist cmd/setup/payload

echo "[1/5] Зависимости"
go mod tidy

echo "[2/5] Ресурсы Windows (иконка, манифест, версия)"
go run ./tools/genres -icon Icon.png -out rsrc_windows_amd64.syso -desc "HealCoreOps" -name HealCoreOps.exe -version $VERSION
go run ./tools/genres -icon Icon.png -out cmd/setup/rsrc_windows_amd64.syso -desc "HealCoreOps Setup" -name HealCoreOps-Setup.exe -version $VERSION
cp Icon.png cmd/setup/icon.png

echo "[3/5] Приложение для Windows"
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o cmd/setup/payload/HealCoreOps.exe .

echo "[4/5] Установщик"
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/HealCoreOps-Setup.exe ./cmd/setup

echo "[5/5] Версия для Linux"
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/healcoreops-linux-amd64 .

ls -lh dist
