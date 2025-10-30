package bot

import (
	"log"

	"tgbot/internal/handlers"
	"tgbot/internal/services/docker"
	"tgbot/internal/services/system"
	"tgbot/pkg/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Bot основная структура бота
type Bot struct {
	api            *tgbotapi.BotAPI
	config         *config.Config
	commandHandler *handlers.CommandHandler
	systemService  *system.Monitor
	dockerService  *docker.Manager
}

// NewBot создает нового бота
func NewBot(cfg *config.Config) (*Bot, error) {
	// Создание API клиента
	api, err := tgbotapi.NewBotAPI(cfg.Bot.Token)
	if err != nil {
		return nil, err
	}

	// Вывод информации о боте
	log.Printf("Авторизован как %s", api.Self.UserName)

	// Создание сервисов
	systemService := system.NewMonitor()
	dockerService, err := docker.NewManager(cfg.Docker.Socket)
	if err != nil {
		return nil, err
	}

	// Создание обработчика команд
	commandHandler := handlers.NewCommandHandler(api, systemService, dockerService)

	return &Bot{
		api:            api,
		config:         cfg,
		commandHandler: commandHandler,
		systemService:  systemService,
		dockerService:  dockerService,
	}, nil
}

// Start запускает бота
func (b *Bot) Start() error {
	// Настройка получения обновлений
	u := tgbotapi.NewUpdate(0)
	u.Timeout = b.config.Bot.UpdateTimeout

	// Получение канала обновлений
	updates, err := b.api.GetUpdatesChan(u)
	if err != nil {
		return err
	}

	// Обработка обновлений
	for update := range updates {
		if update.Message != nil {
			// Проверка авторизации
			if !b.isAuthorized(update.Message.Chat.ID) {
				continue
			}

			// Обработка команд
			b.commandHandler.HandleCommand(update)
		} else if update.CallbackQuery != nil {
			// Проверка авторизации
			if !b.isAuthorized(update.CallbackQuery.Message.Chat.ID) {
				continue
			}

			// Обработка callback запросов
			b.handleCallback(update)
		}
	}

	return nil
}

// Stop останавливает бота
func (b *Bot) Stop() {
	// Закрытие канала обновлений
	b.api.StopReceivingUpdates()
}

// GetAPI возвращает API клиента бота
func (b *Bot) GetAPI() *tgbotapi.BotAPI {
	return b.api
}

// isAuthorized проверяет, авторизован ли пользователь
func (b *Bot) isAuthorized(chatID int64) bool {
	for _, id := range b.config.Bot.AllowedChats {
		if id == chatID {
			return true
		}
	}
	return false
}

// handleCallback обрабатывает callback запросы
func (b *Bot) handleCallback(update tgbotapi.Update) {
	// Передаем обработку всех callback-запросов в commandHandler
	b.commandHandler.HandleCommand(update)
}
