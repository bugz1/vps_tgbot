#!/bin/bash

# Скрипт развертывания Telegram Server Bot на сервере с поддержкой переменных окружения

set -e

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Функция для цветного вывода
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Параметры подключения к серверу (можно переопределить через переменные окружения)
SERVER_IP="${DEPLOY_SERVER_IP:-192.168.1.1}"
SSH_USER="${DEPLOY_SSH_USER:-root}"
SSH_KEY="${DEPLOY_SSH_KEY:-$HOME/.ssh/id_ed25519}"
SERVICE_NAME="${DEPLOY_SERVICE_NAME:-server-bot}"
REMOTE_BINARY_PATH="${DEPLOY_REMOTE_BINARY_PATH:-/usr/local/bin/server-bot}"
REMOTE_CONFIG_PATH="${DEPLOY_REMOTE_CONFIG_PATH:-/etc/server-bot.yaml}"
REMOTE_SERVICE_PATH="${DEPLOY_REMOTE_SERVICE_PATH:-/etc/systemd/system/server-bot.service}"
REMOTE_WORKING_DIR="${DEPLOY_REMOTE_WORKING_DIR:-/opt/telegram-bot}"
SERVICE_USER="${DEPLOY_SERVICE_USER:-root}"
SERVICE_GROUP="${DEPLOY_SERVICE_GROUP:-root}"

# Извлечение директорий из путей к файлам
REMOTE_BINARY_DIR=$(dirname "$REMOTE_BINARY_PATH")
REMOTE_CONFIG_DIR=$(dirname "$REMOTE_CONFIG_PATH")
REMOTE_SERVICE_DIR=$(dirname "$REMOTE_SERVICE_PATH")

print_info "Начинаем развертывание Telegram Server Bot на сервере $SERVER_IP..."
print_info "Используемые параметры:"
echo -e "  ${BLUE}Сервер${NC}: $SERVER_IP"
echo -e "  ${BLUE}SSH пользователь${NC}: $SSH_USER"
echo -e "  ${BLUE}SSH ключ${NC}: $SSH_KEY"
echo -e "  ${BLUE}Имя сервиса${NC}: $SERVICE_NAME"
echo -e "  ${BLUE}Путь к бинарному файлу на сервере${NC}: $REMOTE_BINARY_PATH"
echo -e "  ${BLUE}Путь к конфигурационному файлу на сервере${NC}: $REMOTE_CONFIG_PATH"
echo -e "  ${BLUE}Путь к файлу сервиса на сервере${NC}: $REMOTE_SERVICE_PATH"
echo -e "  ${BLUE}Рабочая директория на сервере${NC}: $REMOTE_WORKING_DIR"
echo -e "  ${BLUE}Пользователь сервиса${NC}: $SERVICE_USER"
echo -e "  ${BLUE}Группа сервиса${NC}: $SERVICE_GROUP"

# Создание необходимых директорий на сервере
print_info "Создаем необходимые директории на сервере..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo mkdir -p '$REMOTE_BINARY_DIR' '$REMOTE_CONFIG_DIR' '$REMOTE_SERVICE_DIR' '$REMOTE_WORKING_DIR'"

# Остановка службы бота
print_info "Останавливаем службу $SERVICE_NAME..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl stop $SERVICE_NAME" || true

# Копирование нового бинарного файла на сервер
print_info "Копируем новый бинарный файл на сервер..."
scp -i "$SSH_KEY" releases/server-bot-linux-amd64 "$SSH_USER@$SERVER_IP:$REMOTE_BINARY_PATH"

# Копирование конфигурационного файла на сервер
print_info "Копируем конфигурационный файл на сервер..."
scp -i "$SSH_KEY" config-server.yaml "$SSH_USER@$SERVER_IP:$REMOTE_CONFIG_PATH"

# Генерация и копирование файла службы на сервер
print_info "Генерируем и копируем файл службы на сервер..."
cat << EOF > /tmp/server-bot.service
[Unit]
Description=Telegram Server Bot
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
ExecStart=$REMOTE_BINARY_PATH
WorkingDirectory=$REMOTE_WORKING_DIR
Restart=always
RestartSec=10
Environment=CONFIG_PATH=$REMOTE_CONFIG_PATH

[Install]
WantedBy=multi-user.target
EOF

scp -i "$SSH_KEY" /tmp/server-bot.service "$SSH_USER@$SERVER_IP:$REMOTE_SERVICE_PATH"
rm /tmp/server-bot.service

# Установка прав на выполнение
print_info "Устанавливаем права на выполнение..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "chmod +x $REMOTE_BINARY_PATH"

# Перезагрузка конфигурации systemd
print_info "Перезагружаем конфигурацию systemd..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl daemon-reload"

# Запуск службы бота
print_info "Запускаем службу $SERVICE_NAME..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl start $SERVICE_NAME"

# Проверка статуса службы
print_info "Проверяем статус службы..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl status $SERVICE_NAME --no-pager || true"

print_success "Развертывание завершено успешно!"