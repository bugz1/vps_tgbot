package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	// Импортируем наш пакет context для работы с traceId
	contextpkg "tgbot/internal/context"
)

// Level представляет уровень важности лога
type Level int32

const (
	// Debug уровень для отладочных сообщений, используется для детального логирования
	Debug Level = iota
	// Info уровень для информационных сообщений, используется для обычных событий
	Info
	// Warn уровень для предупреждений, используется для не критичных проблем
	Warn
	// Error уровень для ошибок, используется для критичных проблем
	Error
)

// levelInt глобальная переменная для хранения текущего уровня логирования
// Используется атомарная операция для потокобезопасного доступа
var levelInt int32 = int32(Info)

// SetLevel устанавливает глобальный уровень логирования
// Принимает регистронезависимые названия уровней: debug, info, warn, error
// name - строка с названием уровня логирования
func SetLevel(name string) {
	// Приводим название к нижнему регистру и удаляем пробелы
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		// Устанавливаем уровень Debug
		atomic.StoreInt32(&levelInt, int32(Debug))
	case "info":
		// Устанавливаем уровень Info
		atomic.StoreInt32(&levelInt, int32(Info))
	case "warn", "warning":
		// Устанавливаем уровень Warn
		atomic.StoreInt32(&levelInt, int32(Warn))
	case "error":
		// Устанавливаем уровень Error
		atomic.StoreInt32(&levelInt, int32(Error))
	default:
		// Если уровень не распознан, оставляем уровень по умолчанию (Info)
		// unknown -> keep default (Info)
	}
}

// getLevel возвращает текущий уровень логирования
// Используется атомарная операция для потокобезопасного доступа
// Возвращает текущий уровень логирования
func getLevel() Level {
	return Level(atomic.LoadInt32(&levelInt))
}

// logEntry структура JSON для строк лога
// Используется для форматирования логов в структурированный JSON формат
type logEntry struct {
	Time    string                 `json:"time"`              // Время записи лога в формате RFC3339Nano
	Level   string                 `json:"level"`             // Уровень логирования (debug, info, warn, error)
	Msg     string                 `json:"msg"`               // Сообщение лога
	TraceID string                 `json:"traceId,omitempty"` // Идентификатор трассировки (если есть)
	Fields  map[string]interface{} `json:"fields,omitempty"`  // Дополнительные поля лога
}

// levelName возвращает строковое представление уровня логирования
// l - уровень логирования
// Возвращает строку с названием уровня
func levelName(l Level) string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return "info"
	}
}

// LogWithCtx записывает структурированную строку JSON с учетом контекста
// Если уровень >= настроенному уровню логирования
// ctx - контекст, из которого может быть извлечен traceId
// l - уровень логирования
// msg - сообщение лога
// fields - дополнительные поля лога
func LogWithCtx(ctx context.Context, l Level, msg string, fields map[string]interface{}) {
	// Проверяем, нужно ли записывать лог на основе уровня
	if l < getLevel() {
		return
	}

	// Создаем структуру лога
	e := logEntry{
		Time:   time.Now().UTC().Format(time.RFC3339Nano), // Текущее время в формате RFC3339Nano
		Level:  levelName(l),                              // Уровень логирования
		Msg:    msg,                                       // Сообщение лога
		Fields: fields,                                    // Дополнительные поля
	}

	// Извлекаем traceId из контекста, если он есть
	if traceID := contextpkg.GetTraceID(ctx); traceID != "" {
		e.TraceID = traceID // Добавляем traceId в лог
	}

	// Сериализуем структуру в JSON
	b, err := json.Marshal(e)
	if err != nil {
		// Если сериализация не удалась, используем резервный формат вывода
		fmt.Fprintf(os.Stdout, "%s [%s] %s traceId=%s %v\n", e.Time, e.Level, e.Msg, e.TraceID, e.Fields)
		return
	}

	// Записываем JSON в stdout
	os.Stdout.Write(b)
	os.Stdout.Write([]byte("\n")) // Добавляем новую строку
}

// Log записывает структурированную строку JSON
// Если уровень >= настроенному уровню логирования
// l - уровень логирования
// msg - сообщение лога
// fields - дополнительные поля лога
func Log(l Level, msg string, fields map[string]interface{}) {
	// Вызываем LogWithCtx с пустым контекстом
	LogWithCtx(context.Background(), l, msg, fields)
}

// Note: convenience wrappers intentionally omitted to avoid symbol name collisions

// MaskKey маскирует чувствительные ключи, такие как публичные ключи WireGuard или токены
// Сохраняет небольшой префикс и суффикс, заменяя середину звездочками
// Примеры:
// - короткие строки (<=8) -> "****"
// - длинные -> сохраняем первые 4 и последние 4 символа
// s - строка для маскирования
// Возвращает замаскированную строку
func MaskKey(s string) string {
	// Если строка пустая, возвращаем как есть
	if s == "" {
		return s
	}

	// Если строка уже выглядит замаскированной, возвращаем как есть
	if strings.Contains(s, "*") {
		return s
	}

	// Получаем длину строки
	n := len(s)

	// Если строка короче или равна 8 символам, заменяем на "****"
	if n <= 8 {
		return "****"
	}

	// Определяем длину префикса и суффикса
	prefix := 4
	suffix := 4

	// Если строка слишком короткая для префикса и суффикса, заменяем на "****"
	if n < prefix+suffix+1 {
		// fallback
		return "****"
	}

	// Возвращаем строку с замаскированной серединой
	return s[:prefix] + strings.Repeat("*", n-prefix-suffix) + s[n-suffix:]
}

// MaskSensitiveFields вспомогательная функция для преобразования полей, где определенные ключи должны быть замаскированы
// Если ключ содержит 'key' или 'token' или 'public', значение будет замаскировано
// fields - исходные поля
// Возвращает новые поля с замаскированными значениями
func MaskSensitiveFields(fields map[string]interface{}) map[string]interface{} {
	// Если поля отсутствуют, возвращаем nil
	if fields == nil {
		return nil
	}

	// Создаем новую карту для результатов
	out := make(map[string]interface{}, len(fields))

	// Проходим по всем полям
	for k, v := range fields {
		// Приводим ключ к нижнему регистру для сравнения
		low := strings.ToLower(k)

		// Обрабатываем значение в зависимости от его типа
		switch val := v.(type) {
		case string:
			// Если ключ содержит 'key', 'token' или 'public', маскируем значение
			if strings.Contains(low, "key") || strings.Contains(low, "token") || strings.Contains(low, "public") {
				out[k] = MaskKey(val) // Замаскированное значение
				continue
			}
			out[k] = val // Не маскируем значение
		default:
			out[k] = val // Для других типов оставляем как есть
		}
	}

	return out
}
