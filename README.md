# Telegram Server Bot

Telegram бот для мониторинга и управления Linux сервером.

## Функциональность

### Мониторинг системы
- Статус CPU: загрузка, количество ядер, частота
- Статус RAM: общий объем, использовано, свободно, swap
- Статус дисков: использование по разделам
- Общая информация о системе

### Управление контейнерами
- Список контейнеров с интерактивным меню
- Действия: start, stop, restart
- Просмотр логов контейнеров

### Управление системой
- Перезагрузка сервера (с подтверждением)
- Выключение сервера (с подтверждением)
- Проверка доступных обновлений системы
- Обновление системы

### Мониторинг и уведомления
- Мониторинг системных метрик (CPU, RAM, диск)
- Уведомления о достижении пороговых значений
- Настройка пороговых значений в конфигурации
## Установка

### Вариант 1: Использование скрипта установки

1. Запустите скрипт установки:
   ```bash
   sudo ./install.sh
   ```
2. Отредактируйте `/opt/telegram-bot/config.yaml`, указав ваши параметры
3. Запустите сервис:
   ```bash
   sudo systemctl start server-bot.service
   ```

## Установка


### Вариант 1: Использование готового бинарного файла

1. Скачайте архив `server-bot-linux-amd64.tar.gz`
2. Распакуйте архив:
   ```bash
   tar -xzf server-bot-linux-amd64.tar.gz
   ```
3. Отредактируйте `config.yaml.example`, указав ваши параметры, и переименуйте в `config.yaml`
4. Следуйте инструкциям по настройке systemd из раздела "Настройка systemd"

### Вариант 2: Сборка из исходного кода

1. Клонируйте репозиторий:
   ```bash
   git clone <repository-url>
   cd tgbot
   ```

2. Создайте файл конфигурации `config.yaml` на основе примера:
   ```bash
   cp config.yaml.example config.yaml
   ```
   Затем отредактируйте `config.yaml`, указав ваши параметры.
3. Соберите проект для разных архитектур:
   ```bash
   ./build.sh
   ```
   Или соберите вручную для нужной архитектуры:
   ```bash
   GOOS=linux GOARCH=amd64 go build -o server-bot ./cmd/bot
   ```


4. Создайте пользователя для запуска бота:
   ```bash
   sudo useradd -r -s /bin/false telegram-bot
   ```

5. Добавьте пользователя в группу docker:
   ```bash
   sudo usermod -aG docker telegram-bot
   ```

6. Скопируйте бинарный файл и конфигурацию в системную директорию:
   ```bash
   sudo mkdir -p /opt/telegram-bot
   sudo cp server-bot /opt/telegram-bot/
   sudo cp config.yaml /opt/telegram-bot/
   sudo chown -R telegram-bot:telegram-bot /opt/telegram-bot
   ```

7. Скопируйте systemd service файл:
   ```bash
   sudo cp server-bot.service /etc/systemd/system/
   ```

8. Запустите службу:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable server-bot
   sudo systemctl start server-bot
   ```

## Настройка systemd

Для запуска бота как службы systemd:

1. Создайте пользователя для запуска бота (если еще не создан):
   ```bash
   sudo useradd -r -s /bin/false telegram-bot
   ```

2. Добавьте пользователя в группу docker:
   ```bash
   sudo usermod -aG docker telegram-bot
   ```

3. Скопируйте бинарный файл и конфигурацию в системную директорию:
   ```bash
   sudo mkdir -p /opt/telegram-bot
   sudo cp server-bot /opt/telegram-bot/
   sudo cp config.yaml /opt/telegram-bot/
   sudo chown -R telegram-bot:telegram-bot /opt/telegram-bot
   ```

4. Скопируйте systemd service файл:
   ```bash
   sudo cp server-bot.service /etc/systemd/system/
   ```

5. Запустите службу:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable server-bot
   sudo systemctl start server-bot
   ```

## Пример конфигурационного файла

```yaml
bot:
  token: "1234567890:ABCDEF1234567890ABCDEF1234567890ABC"  # Токен вашего Telegram бота
  allowed_chats: [123456789, 987654321]  # Список ID чатов, которым разрешено использовать бота
  update_timeout: 60  # Таймаут для получения обновлений от Telegram API

monitoring:
  check_interval: 30  # Интервал проверки системных метрик (в секундах)
  cpu_threshold: 90   # Порог загрузки CPU для уведомлений (в процентах)
  memory_threshold: 90 # Порог использования памяти для уведомлений (в процентах)
  disk_threshold: 10  # Порог свободного места на диске для уведомлений (в процентах)

docker:
  socket: "/var/run/docker.sock"  # Путь к Docker socket
  timeout: 30  # Таймаут для операций с Docker (в секундах)
```

## Требования

- Linux сервер с Docker
- Go 1.19+ (для сборки)
- Доступ к Telegram API

## Безопасность

- Авторизация по whitelist chat ID
- Подтверждение для критических команд
- Логирование всех операций