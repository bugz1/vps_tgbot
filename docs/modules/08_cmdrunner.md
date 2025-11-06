# Модуль: internal/cmdrunner

## Описание

Универсальный модуль для безопасного выполнения внешних команд с поддержкой таймаутов, повторных попыток (retry) и интерактивного запроса sudo-пароля через Telegram.

## Структура

```go
type RunOptions struct {
    Timeout           time.Duration
    Attempts          int
    Password          string
    PasswordFromEnv   bool
    PasswordFromConfig bool
    Requester         PasswordRequester
    ChatID            int64
}

type PasswordRequester interface {
    RequestPassword(chatID int64) (string, error)
}
```

## Принцип работы

### Основная функция (`RunWithRetries`)

```mermaid
flowchart TD
    A[RunWithRetries] --> B{cmdParts пуст?}
    B -->|Да| C[Ошибка: пустой список]
    B -->|Нет| D[Заполнение дефолтов]
    D --> E[Цикл попыток]
    E --> F[Создание контекста с таймаутом]
    F --> G{Команда sudo?}
    G -->|Да| H{Пароль известен?}
    G -->|Нет| I[Запуск команды]
    H -->|Да| J[Добавление -S и stdin]
    H -->|Нет| I
    J --> I
    I --> K[Ожидание завершения]
    K --> L{Успех?}
    L -->|Да| M[Возврат результата]
    L -->|Нет| N{Таймаут?}
    N -->|Да| O[Ошибка таймаута]
    N -->|Нет| P{sudo запросил пароль?}
    P -->|Да| Q[Запрос пароля через Telegram]
    Q --> R[Повтор команды с паролем]
    R --> S{Успех?}
    S -->|Да| M
    S -->|Нет| T[Ошибка]
    P -->|Нет| U{Есть ещё попытки?}
    U -->|Да| V[Задержка и повтор]
    U -->|Нет| T
    V --> E
```

### Заполнение дефолтов

Если опции не заданы, берутся из конфига:
- `Timeout` → `cmdrunner.timeout_seconds` (по умолчанию 10 сек)
- `Attempts` → `cmdrunner.attempts` (по умолчанию 1)

### Обработка sudo-пароля

#### Приоритет источников пароля

1. `opts.Password` — явно переданный пароль
2. `SUDO_PASSWORD` — переменная окружения (если `PasswordFromEnv = true`)
3. `cmdrunner.sudo_password` — из конфига (если `PasswordFromConfig = true`)
4. Запрос через Telegram (если `Requester != nil` и `ChatID != 0`)

#### Логика "пароль по требованию"

```mermaid
sequenceDiagram
    participant Runner
    participant Command
    participant Telegram
    
    Runner->>Command: Запуск sudo без пароля
    Command-->>Runner: stderr: "password is required"
    Runner->>Runner: Обнаружение запроса пароля
    Runner->>Telegram: RequestPassword(chatID)
    Telegram-->>Runner: Пароль от пользователя
    Runner->>Command: Запуск sudo -S с паролем в stdin
    Command-->>Runner: Успех
```

**Определение запроса пароля**:
- Проверка stderr на наличие фраз:
  - `"password is required"`
  - `"a password is required"`
  - `"sudo:"` + `"password"`
  - `"no tty present and no askpass program specified"`
  - `"try again."`

**Если пароль найден заранее**:
- Команда сразу модифицируется: добавляется `-S` после `sudo`
- Пароль передаётся в stdin

### Retry механизм

```go
for i := 0; i < opts.Attempts; i++ {
    // Выполнение команды
    if err == nil {
        return result, nil
    }
    // Задержка перед следующей попыткой
    time.Sleep(time.Duration(200*attemptNum) * time.Millisecond)
}
```

Задержка увеличивается с каждой попыткой: 200ms, 400ms, 600ms...

### Захват вывода

```go
var stdoutBuf, stderrBuf bytes.Buffer
cmd.Stdout = &stdoutBuf
cmd.Stderr = &stderrBuf
```

Результат объединяется: `stdout + "\n" + stderr` (если stderr не пуст).

### Логирование

Логируется каждая попытка:
- Команда (маскируются чувствительные данные)
- Номер попытки
- Код возврата
- stdout/stderr (обрезаются до 2000 символов)

## Взаимодействие с другими модулями

```mermaid
graph TD
    A[cmdrunner] --> B[os/exec]
    A --> C[pkg/logger]
    A --> D[viper]
    A --> E[password.Requester]
    
    F[services/system] --> A
    G[services/docker] --> A
    H[services/amnezia] --> A
```

## Зависимости

- `os/exec` — выполнение команд
- `pkg/logger` — логирование
- `github.com/spf13/viper` — конфигурация
- `internal/password` — запрос паролей через Telegram

## Особенности

### Безопасность

- Пароли маскируются в логах через `logger.MaskSensitiveFields`
- Пароль передаётся только через stdin, не через аргументы командной строки
- Чувствительные данные (ключи, токены) автоматически маскируются

### Таймауты

- Каждая попытка имеет свой таймаут
- Таймаут задаётся через контекст (`context.WithTimeout`)
- При превышении таймаута команда убивается

### Обработка ошибок

- Разделение stdout и stderr для корректной диагностики
- Детальное логирование всех попыток
- Возврат последней ошибки при исчерпании попыток

## Примеры использования

### Простая команда

```go
parts := []string{"ls", "-la"}
opts := cmdrunner.RunOptions{
    Timeout: 10 * time.Second,
    Attempts: 1,
}
output, err := cmdrunner.RunWithRetries(ctx, parts, opts)
```

### Команда с sudo (пароль из конфига)

```go
parts := []string{"sudo", "systemctl", "restart", "nginx"}
opts := cmdrunner.RunOptions{
    Timeout: 30 * time.Second,
    Attempts: 3,
    PasswordFromConfig: true,
}
output, err := cmdrunner.RunWithRetries(ctx, parts, opts)
```

### Команда с запросом пароля через Telegram

```go
parts := []string{"sudo", "reboot"}
opts := cmdrunner.RunOptions{
    Timeout: 10 * time.Second,
    Attempts: 1,
    Requester: passwordRequester,
    ChatID: chatID,
}
output, err := cmdrunner.RunWithRetries(ctx, parts, opts)
```

## Конфигурация

```yaml
cmdrunner:
  timeout_seconds: 30
  attempts: 3
  sudo_password: ""  # Опционально
```

## Логирование

События:
- `cmdrunner.start_failed` — ошибка запуска команды
- `cmdrunner.attempt` — результат попытки (debug уровень)
- `cmdrunner.task_timeout` — таймаут задачи (workerpool)

