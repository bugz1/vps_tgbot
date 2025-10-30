#!/bin/bash

# Скрипт сборки Telegram Server Bot для разных архитектур

set -e

echo "Сборка Telegram Server Bot..."

# Создание директории для релизов
mkdir -p releases

# Сборка для Linux AMD64
echo "Сборка для Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -o releases/server-bot cmd/bot/main.go
tar -czf releases/server-bot-linux-amd64.tar.gz -C releases server-bot

echo "Сборка завершена! Файлы находятся в директории releases."