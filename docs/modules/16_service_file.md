# Модуль: server-bot.service

## Описание

Systemd unit файл для управления Telegram Server Bot как системным сервисом. Отвечает за автоматический запуск бота при загрузке системы, перезапуск при сбоях и управление рабочим окружением сервиса.

## Структура

```ini
[Unit]
Description=Telegram Server Bot
After=network.target

[Service]
Type=simple
User=root
Group=root
ExecStart=/usr/local/bin/server-bot
WorkingDirectory=/opt/telegram-bot
Restart=always
RestartSec=10
Environment=CONFIG_PATH=/etc/server-bot.yaml
Environment=LOG_LEVEL=debug

[Install]
WantedBy=multi-user.target
```

## Принцип работы

### Секция [Unit]

- `Description=Telegram Server Bot` — описание сервиса, отображаемое в системных логах и при управлении сервисом
- `After=network.target` — указывает, что сервис должен запускаться после того, как сетевые интерфейсы будут настроены

### Секция [Service]

- `Type=simple` — указывает, что процесс запускается немедленно и не разветвляется
- `User=root` — пользователь, от имени которого запускается сервис (root)
- `Group=root` — группа, от имени которой запускается сервис (root)
- `ExecStart=/usr/local/bin/server-bot` — команда запуска бота
- `WorkingDirectory=/opt/telegram-bot` — рабочая директория сервиса
- `Restart=always` — политика перезапуска сервиса при завершении (всегда перезапускать)
- `RestartSec=10` — задержка перед перезапуском сервиса (10 секунд)
- `Environment=CONFIG_PATH=/etc/server-bot.yaml` — переменная окружения, указывающая путь к конфигурационному файлу

### Секция [Install]

- `WantedBy=multi-user.target` — указывает, что сервис должен быть запущен в многопользовательском режиме

## Взаимодействие с другими модулями

```mermaid
graph TD
    A[server-bot.service] --> B[/usr/local/bin/server-bot]
    A --> C[/etc/server-bot.yaml]
    A --> D[/opt/telegram-bot]
    A --> E[systemd]
    
    E --> F[server-bot process]
```

## Переменные окружения

Сервис использует переменную окружения для указания пути к конфигурационному файлу:

- `CONFIG_PATH` — путь к конфигурационному файлу (по умолчанию: `/etc/server-bot.yaml`)

## Особенности

- Автоматический запуск при загрузке системы
- Перезапуск при сбоях с задержкой 10 секунд
- Работа от имени root для доступа к системным ресурсам
- Использование фиксированной рабочей директории
- Поддержка указания пути к конфигурационному файлу через переменную окружения

## Управление сервисом

Для управления сервисом используются команды systemctl:

```bash
# Запуск сервиса
sudo systemctl start server-bot

# Остановка сервиса
sudo systemctl stop server-bot

# Перезапуск сервиса
sudo systemctl restart server-bot

# Проверка статуса сервиса
sudo systemctl status server-bot

# Включение автозапуска сервиса
sudo systemctl enable server-bot

# Отключение автозапуска сервиса
sudo systemctl disable server-bot

# Просмотр логов сервиса
sudo journalctl -u server-bot -f
```

## Возможные проблемы и решения

1. **Сервис не запускается**:
   - Проверьте, что бинарный файл существует по пути `/usr/local/bin/server-bot`
   - Убедитесь, что файл имеет права на выполнение
   - Проверьте логи сервиса: `sudo journalctl -u server-bot -f`

2. **Ошибка доступа к конфигурационному файлу**:
   - Проверьте, что конфигурационный файл существует по пути `/etc/server-bot.yaml`
   - Убедитесь, что у сервиса есть права на чтение файла

3. **Сервис постоянно перезапускается**:
   - Проверьте логи сервиса на наличие ошибок в работе бота
   - Убедитесь, что все зависимости бота доступны (Docker, доступ к Telegram API и т.д.)

4. **Сервис не запускается при загрузке системы**:
   - Проверьте, что сервис включен: `sudo systemctl is-enabled server-bot`
   - При необходимости включите автозапуск: `sudo systemctl enable server-bot`