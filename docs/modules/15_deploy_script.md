# Модуль: deploy.sh

## Описание

Скрипт развертывания Telegram Server Bot на сервере через SSH. Отвечает за копирование бинарного файла, конфигурации и генерацию systemd unit файла на удаленный сервер, а также за управление сервисом. Поддерживает настройку через переменные окружения.

## Структура

```bash
#!/bin/bash

# Скрипт развертывания Telegram Server Bot на сервере с поддержкой переменных окружения

set -e

# Параметры подключения к серверу (можно переопределить через переменные окружения)
SERVER_IP="${DEPLOY_SERVER_IP:-<ip>}"
SSH_USER="${DEPLOY_SSH_USER:-root}"
SSH_KEY="${DEPLOY_SSH_KEY:-$HOME/.ssh/id_ed25519}"
SERVICE_NAME="${DEPLOY_SERVICE_NAME:-server-bot}"
REMOTE_BINARY_PATH="${DEPLOY_REMOTE_BINARY_PATH:-/usr/local/bin/server-bot}"
REMOTE_CONFIG_PATH="${DEPLOY_REMOTE_CONFIG_PATH:-/etc/server-bot.yaml}"
REMOTE_SERVICE_PATH="${DEPLOY_REMOTE_SERVICE_PATH:-/etc/systemd/system/server-bot.service}"
REMOTE_WORKING_DIR="${DEPLOY_REMOTE_WORKING_DIR:-/opt/telegram-bot}"

# Извлечение директорий из путей к файлам
REMOTE_BINARY_DIR=$(dirname "$REMOTE_BINARY_PATH")
REMOTE_CONFIG_DIR=$(dirname "$REMOTE_CONFIG_PATH")
REMOTE_SERVICE_DIR=$(dirname "$REMOTE_SERVICE_PATH")

echo "Начинаем развертывание Telegram Server Bot на сервере $SERVER_IP..."
echo "Используемые параметры:"
echo "  Сервер: $SERVER_IP"
echo "  SSH пользователь: $SSH_USER"
echo "  SSH ключ: $SSH_KEY"
echo "  Имя сервиса: $SERVICE_NAME"
echo "  Путь к бинарному файлу на сервере: $REMOTE_BINARY_PATH"
echo "  Путь к конфигурационному файлу на сервере: $REMOTE_CONFIG_PATH"
echo "  Путь к файлу сервиса на сервере: $REMOTE_SERVICE_PATH"
echo "  Рабочая директория на сервере: $REMOTE_WORKING_DIR"

# Создание необходимых директорий на сервере
echo "Создаем необходимые директории на сервере..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo mkdir -p '$REMOTE_BINARY_DIR' '$REMOTE_CONFIG_DIR' '$REMOTE_SERVICE_DIR' '$REMOTE_WORKING_DIR'"

# Остановка службы бота
echo "Останавливаем службу $SERVICE_NAME..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl stop $SERVICE_NAME" || true

# Копирование нового бинарного файла на сервер
echo "Копируем новый бинарный файл на сервер..."
scp -i "$SSH_KEY" releases/server-bot-linux-amd64 "$SSH_USER@$SERVER_IP:$REMOTE_BINARY_PATH"

# Копирование конфигурационного файла на сервер
echo "Копируем конфигурационный файл на сервер..."
scp -i "$SSH_KEY" config-server.yaml "$SSH_USER@$SERVER_IP:$REMOTE_CONFIG_PATH"

# Генерация и копирование файла службы на сервер
echo "Генерируем и копируем файл службы на сервер..."
cat << EOF > /tmp/server-bot.service
[Unit]
Description=Telegram Server Bot
After=network.target

[Service]
Type=simple
User=root
Group=root
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
echo "Устанавливаем права на выполнение..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "chmod +x $REMOTE_BINARY_PATH"

# Перезагрузка конфигурации systemd
echo "Перезагружаем конфигурацию systemd..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl daemon-reload"

# Запуск службы бота
echo "Запускаем службу $SERVICE_NAME..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl start $SERVICE_NAME"

# Проверка статуса службы
echo "Проверяем статус службы..."
ssh -i "$SSH_KEY" "$SSH_USER@$SERVER_IP" "sudo systemctl status $SERVICE_NAME --no-pager || true"

echo "Развертывание завершено успешно!"
```

## Принцип работы

### Подготовка

1. Установка флага `set -e` для немедленного завершения при ошибках
2. Определение переменных подключения к серверу с возможностью переопределения через переменные окружения
3. Извлечение директорий из путей к файлам для последующего создания
4. Вывод приветственного сообщения и используемых параметров

### Развертывание

1. **Создание необходимых директорий на сервере**:
   - Извлекает директории из путей к файлам
   - Создает все необходимые директории на сервере с помощью `sudo mkdir -p`

2. **Остановка службы бота**:
   - Выполняет команду `sudo systemctl stop $SERVICE_NAME` на удаленном сервере
   - Использует `|| true` для предотвращения завершения скрипта при ошибках

3. **Копирование бинарного файла**:
   - Копирует собранный бинарный файл `releases/server-bot-linux-amd64` на удаленный сервер
   - Использует `scp` с указанием SSH ключа для аутентификации

4. **Копирование конфигурационного файла**:
   - Копирует файл `config-server.yaml` на удаленный сервер
   - Размещает файл по пути, указанному в переменной `REMOTE_CONFIG_PATH`

5. **Генерация и копирование файла службы**:
   - Генерирует systemd unit файл с использованием заданных параметров
   - Копирует сгенерированный файл на удаленный сервер

6. **Установка прав на выполнение**:
   - Устанавливает права на выполнение для бинарного файла на удаленном сервере

7. **Перезагрузка конфигурации systemd**:
   - Выполняет команду `sudo systemctl daemon-reload` на удаленном сервере

8. **Запуск службы бота**:
   - Выполняет команду `sudo systemctl start $SERVICE_NAME` на удаленном сервере

9. **Проверка статуса службы**:
   - Выполняет команду `sudo systemctl status $SERVICE_NAME --no-pager` на удаленном сервере
   - Использует `|| true` для предотвращения завершения скрипта при ошибках

### Результат

Сервис Telegram Server Bot обновлен и запущен на удаленном сервере.

## Переменные окружения

Скрипт поддерживает настройку через переменные окружения, что позволяет гибко конфигурировать процесс развертывания:

| Переменная | Назначение | Значение по умолчанию |
|------------|------------|-----------------------|
| `DEPLOY_SERVER_IP` | IP-адрес или домен сервера | `<ip>` |
| `DEPLOY_SSH_USER` | Пользователь SSH | `root` |
| `DEPLOY_SSH_KEY` | Путь к SSH ключу | `$HOME/.ssh/id_ed25519` |
| `DEPLOY_SERVICE_NAME` | Имя сервиса | `server-bot` |
| `DEPLOY_REMOTE_BINARY_PATH` | Путь к бинарному файлу на сервере | `/usr/local/bin/server-bot` |
| `DEPLOY_REMOTE_CONFIG_PATH` | Путь к конфигурационному файлу на сервере | `/etc/server-bot.yaml` |
| `DEPLOY_REMOTE_SERVICE_PATH` | Путь к файлу сервиса на сервере | `/etc/systemd/system/server-bot.service` |
| `DEPLOY_REMOTE_WORKING_DIR` | Рабочая директория на сервере | `/opt/telegram-bot` |

Пример использования переменных окружения:

```bash
DEPLOY_SERVER_IP=192.168.1.100 DEPLOY_SSH_USER=admin ./deploy.sh
```

## Взаимодействие с другими модулями

```mermaid
graph TD
    A[deploy.sh] --> B[releases/server-bot-linux-amd64]
    A --> C[config-server.yaml]
    A --> D[SSH соединение]
    D --> E[Удаленный сервер]
    E --> F[/usr/local/bin/server-bot]
    E --> G[/etc/server-bot.yaml]
    E --> H[/etc/systemd/system/server-bot.service]
    E --> I[systemd]
    
    I --> J[server-bot service]
```

## Зависимости

- `ssh` — для установления SSH соединения с удаленным сервером
- `scp` — для копирования файлов на удаленный сервер
- Наличие собранных файлов:
  - `releases/server-bot-linux-amd64` (создается скриптом build.sh)
  - `config-server.yaml` (конфигурационный файл)
- Настроенная SSH аутентификация по ключу
- Доступ к удаленному серверу по SSH

## Особенности

- Использует флаг `set -e` для обеспечения надежности выполнения
- Поддерживает настройку через переменные окружения
- Автоматически создает необходимые директории на сервере перед копированием файлов
- Автоматически генерирует systemd unit файл с заданными параметрами
- Автоматически останавливает сервис перед развертыванием
- Автоматически запускает сервис после развертывания
- Перезагружает конфигурацию systemd после обновления файла сервиса
- Проверяет статус сервиса после развертывания
- Использует SSH ключи для аутентификации
- Выводит используемые параметры для отладки

## Возможные проблемы и решения

1. **Ошибка подключения по SSH**:
   - Проверьте, что сервер доступен по указанному IP/домену
   - Убедитесь, что SSH ключ существует и имеет правильные права доступа
   - Проверьте, что пользователь имеет права на подключение по SSH

2. **Отказ в доступе при копировании файлов**:
   - Убедитесь, что у пользователя есть права на запись в указанные директории на сервере
   - Проверьте, что пути к файлам существуют на локальной машине

3. **Сервис не запускается**:
   - Проверьте логи сервиса на удаленном сервере: `sudo journalctl -u server-bot -f`
   - Убедитесь, что конфигурационный файл корректен
   - Проверьте, что порты, указанные в конфигурации, доступны

4. **systemd не находит unit файл**:
   - Убедитесь, что файл `server-bot.service` скопирован в `/etc/systemd/system/`
   - Выполните `sudo systemctl daemon-reload` вручную на сервере

5. **Ошибки при использовании переменных окружения**:
   - Проверьте правильность задания переменных
   - Убедитесь, что указанные пути существуют и доступны

## Расширение функциональности

Для добавления поддержки развертывания на несколько серверов можно модифицировать скрипт следующим образом:

```bash
# Список серверов
SERVERS=("server1.example.com" "server2.example.com" "server3.example.com")

# Развертывание на каждый сервер
for SERVER in "${SERVERS[@]}"; do
    echo "Развертывание на сервер $SERVER..."
    SERVER_IP="$SERVER" ./deploy.sh
done