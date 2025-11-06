package password

import (
	"fmt"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Requester структура для запроса пароля через Telegram
type Requester struct {
	bot       *tgbotapi.BotAPI
	chatID    int64
	password  string
	passwordMu sync.Mutex
	passwordReady chan struct{}
}

// NewRequester создает новый Requester
func NewRequester(bot *tgbotapi.BotAPI) *Requester {
	return &Requester{
		bot:           bot,
		passwordReady: make(chan struct{}, 1),
	}
}

// RequestPassword запрашивает пароль через Telegram
func (r *Requester) RequestPassword(chatID int64) (string, error) {
	// Отправляем сообщение с запросом пароля
	message := "🔐 Для выполнения команды требуется sudo пароль. Пожалуйста, введите пароль:"
	msg := tgbotapi.NewMessage(chatID, message)
	r.bot.Send(msg)

	// Устанавливаем chatID для получения ответа
	r.chatID = chatID

	// Ожидаем ввод пароля от пользователя с таймаутом
	select {
	case <-r.passwordReady:
		r.passwordMu.Lock()
		password := r.password
		r.passwordMu.Unlock()
		return password, nil
	case <-time.After(60 * time.Second):
		return "", fmt.Errorf("таймаут ожидания ввода пароля")
	}
}

// SetPassword устанавливает пароль, полученный от пользователя
func (r *Requester) SetPassword(chatID int64, password string) {
	// Проверяем, что пароль пришел от нужного чата
	if r.chatID != chatID {
		return
	}
	
	r.passwordMu.Lock()
	r.password = password
	r.passwordMu.Unlock()
	
	// Сигнализируем, что пароль готов
	select {
	case r.passwordReady <- struct{}{}:
	default:
	}
}