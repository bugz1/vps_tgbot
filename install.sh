#!/bin/bash

# Скрипт установки Telegram Server Bot

set -e

# Для сервера собираем только под Linux AMD64
BINARY_NAME="server-bot"

echo "Установка Telegram Server Bot для архитектуры x86_64..."

# Создание пользователя и группы для бота
echo "Создание пользователя и группы telegram-bot..."
sudo groupadd -f telegram-bot
sudo useradd -r -g telegram-bot -d /opt/telegram-bot -s /bin/false telegram-bot || true

# Создание директории для бота
echo "Создание директории /opt/telegram-bot..."
sudo mkdir -p /opt/telegram-bot

# Копирование бинарного файла
echo "Копирование бинарного файла..."
sudo cp $BINARY_NAME /usr/local/bin/server-bot
sudo chmod +x /usr/local/bin/server-bot

# Копирование конфигурационного файла
echo "Копирование конфигурационного файла..."
sudo cp config.yaml.example /opt/telegram-bot/config.yaml
sudo chown telegram-bot:telegram-bot /opt/telegram-bot/config.yaml
sudo chmod 600 /opt/telegram-bot/config.yaml

# Копирование systemd сервиса
echo "Копирование systemd сервиса..."
sudo cp server-bot.service /etc/systemd/system/server-bot.service

# Перезагрузка systemd
echo "Перезагрузка systemd..."
sudo systemctl daemon-reload

# Включение сервиса
echo "Включение сервиса..."
sudo systemctl enable server-bot.service

echo "Установка завершена!"
echo "Пожалуйста, отредактируйте /opt/telegram-bot/config.yaml и добавьте токен бота и ID чата."
echo "После этого запустите сервис командой: sudo systemctl start server-bot.service"