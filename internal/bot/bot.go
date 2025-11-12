package bot

import (
	"fmt"
	"tgbot/internal/handlers"
	"tgbot/internal/password"
	"tgbot/internal/services/amnezia"
	"tgbot/internal/services/docker"
	"tgbot/internal/services/system"
	"tgbot/internal/workerpool"
	"tgbot/pkg/config"
	"tgbot/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Bot основная структура бота
type Bot struct {
	api               *tgbotapi.BotAPI
	config            *config.Config
	commandHandler    *handlers.CommandHandler
	systemService     *system.Monitor
	dockerService     *docker.Manager
	amneziaService    *amnezia.Service
	workerPool        *workerpool.WorkerPool
	passwordRequester *password.Requester
}

// NewBot создает нового бота
func NewBot(cfg *config.Config, workerPool *workerpool.WorkerPool) (*Bot, error) {
	// Создание API клиента
	api, err := tgbotapi.NewBotAPI(cfg.Bot.Token)
	if err != nil {
		return nil, err
	}

	// Вывод информации о боте
	logger.Log(logger.Info, "bot.authorized", map[string]interface{}{"username": api.Self.UserName})

	// Создание сервисов
	systemService := system.NewMonitor()
	dockerService, err := docker.NewManager(cfg.Docker.Socket, cfg.Docker.Timeout, cfg.Docker.Bin, cfg.Docker.CommandPrefix)
	if err != nil {
		return nil, err
	}

	// Создание сервиса Amnezia VPN
	amneziaService, err := amnezia.NewService(dockerService)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания сервиса Amnezia VPN: %v", err)
	}

	// Создание Requester для запроса пароля через Telegram
	passwordRequester := password.NewRequester(api)

	// Создание обработчика команд с передачей workerPool и passwordRequester
	commandHandler := handlers.NewCommandHandler(api, systemService, dockerService, amneziaService, workerPool, passwordRequester)

	return &Bot{
		api:               api,
		config:            cfg,
		commandHandler:    commandHandler,
		systemService:     systemService,
		dockerService:     dockerService,
		amneziaService:    amneziaService,
		workerPool:        workerPool,
		passwordRequester: passwordRequester,
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
		} else if update.Message != nil && update.Message.Text != "" {
			// Проверка, является ли сообщение ответом на запрос пароля
			// Это сообщение от авторизованного пользователя, который ввел пароль
			if b.isAuthorized(update.Message.Chat.ID) {
				// Проверяем, ожидаем ли мы ввод пароля от этого пользователя
				// Отправляем пароль в passwordRequester
				b.passwordRequester.SetPassword(update.Message.Chat.ID, update.Message.Text)
			}
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
