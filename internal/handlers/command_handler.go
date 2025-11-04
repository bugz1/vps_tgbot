package handlers

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"tgbot/internal/services/amnezia"
	"tgbot/internal/services/docker"
	"tgbot/internal/services/system"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// CommandHandler обработчик команд
type CommandHandler struct {
	bot            *tgbotapi.BotAPI
	systemService  *system.Monitor
	dockerService  *docker.Manager
	amneziaService *amnezia.Service
	// Храним состояние ожидания ввода от пользователя
	pendingInputs map[int64]PendingInput
}

// PendingInput структура для хранения информации о pending input
type PendingInput struct {
	Action string
	Data   map[string]string
}

// NewCommandHandler создает новый обработчик команд
func NewCommandHandler(bot *tgbotapi.BotAPI, systemService *system.Monitor, dockerService *docker.Manager, amneziaService *amnezia.Service) *CommandHandler {
	return &CommandHandler{
		bot:            bot,
		systemService:  systemService,
		dockerService:  dockerService,
		amneziaService: amneziaService,
		pendingInputs:  make(map[int64]PendingInput),
	}
}

// HandleCommand обрабатывает команды
func (h *CommandHandler) HandleCommand(update tgbotapi.Update) {
	if update.Message != nil {
		// Получение текста команды
		command := strings.TrimSpace(update.Message.Text)
		chatID := update.Message.Chat.ID

		// Проверяем, есть ли ожидание ввода от пользователя
		if pendingInput, exists := h.pendingInputs[chatID]; exists {
			// Обрабатываем ввод пользователя в зависимости от ожидаемого действия
			switch pendingInput.Action {
			case "create_wireguard_client":
				h.handleCreateWireGuardClientInput(update, pendingInput)
			default:
				// Если действие не определено, удаляем pending input и обрабатываем как обычную команду
				delete(h.pendingInputs, chatID)
				h.handleRegularCommand(update, command)
			}
			return
		}

		// Обработка обычных команд
		h.handleRegularCommand(update, command)
	} else if update.CallbackQuery != nil {
		// Обработка callback-запросов
		h.handleCallbackQuery(update)
	}
}

// handleRegularCommand обрабатывает обычные команды
func (h *CommandHandler) handleRegularCommand(update tgbotapi.Update, command string) {
	// Обработка команды
	switch {
	case command == "/start":
		h.handleStart(update)
	case command == "/status":
		h.handleStatus(update)
	case command == "/cpu":
		h.handleCPU(update)
	case command == "/ram":
		h.handleRAM(update)
	case command == "/hdd":
		h.handleHDD(update)
	case command == "/containers":
		h.handleContainers(update)
	case command == "/reboot":
		h.handleReboot(update)
	case command == "/shutdown":
		h.handleShutdown(update)
	default:
		h.handleUnknown(update)
	}
}

// handleStart обрабатывает команду /start
func (h *CommandHandler) handleStart(update tgbotapi.Update) {
	// Создание inline клавиатуры с основными командами
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус системы", "status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🐳 Контейнеры", "containers"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛡️ Amnezia VPN", "amnezia_vpn"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Сервисы", "services"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🖥 Управление сервером", "server_management"),
		),
	)

	message := "🤖 *Telegram Server Bot*\n\nВыберите действие:"
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	h.bot.Send(msg)
}

// handleStatus обрабатывает команду /status
func (h *CommandHandler) handleStatus(update tgbotapi.Update) {
	// Получение информации о системе
	cpuInfo, _ := h.systemService.GetCPUInfoString()
	memInfo, _ := h.systemService.GetMemoryInfoString()
	diskInfo, _ := h.systemService.GetDiskInfoString()

	message := fmt.Sprintf(`📊 *Общий статус системы*

💻 CPU: %s

🧠 RAM: %s

💾 HDD: %s

`, cpuInfo, memInfo, diskInfo)

	h.sendSimpleMessage(update.Message.Chat.ID, message)
}

// handleCPU обрабатывает команду /cpu
func (h *CommandHandler) handleCPU(update tgbotapi.Update) {
	// Получение информации о CPU
	cpuInfo, _ := h.systemService.GetCPUInfoString()

	message := fmt.Sprintf(`💻 *Статус CPU*

%s
`, cpuInfo)

	h.sendSimpleMessage(update.Message.Chat.ID, message)
}

// handleRAM обрабатывает команду /ram
func (h *CommandHandler) handleRAM(update tgbotapi.Update) {
	// Получение информации о памяти
	memInfo, _ := h.systemService.GetMemoryInfoString()

	message := fmt.Sprintf(`🧠 *Статус памяти*

%s
`, memInfo)

	h.sendSimpleMessage(update.Message.Chat.ID, message)
}

// handleHDD обрабатывает команду /hdd
func (h *CommandHandler) handleHDD(update tgbotapi.Update) {
	// Получение информации о дисках
	diskInfo, _ := h.systemService.GetDiskInfoString()

	message := fmt.Sprintf(`💾 *Статус дисков*

%s
`, diskInfo)

	h.sendSimpleMessage(update.Message.Chat.ID, message)
}

// handleContainers обрабатывает команду /containers
func (h *CommandHandler) handleContainers(update tgbotapi.Update) {
	// Получение списка контейнеров
	containers, err := h.dockerService.ListContainers()
	if err != nil {
		message := "❌ Ошибка получения списка контейнеров"
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
		h.bot.Send(msg)
		return
	}

	if len(containers) == 0 {
		message := "📭 Нет запущенных контейнеров"
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
		h.bot.Send(msg)
		return
	}

	// Создание inline клавиатуры
	var keyboard tgbotapi.InlineKeyboardMarkup
	buttons := make([][]tgbotapi.InlineKeyboardButton, 0)

	// Добавляем кнопки для каждого контейнера
	for _, container := range containers {
		button := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s [%s]", container.Name, container.Status),
			fmt.Sprintf("container:%s", container.ID),
		)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(button))
	}

	// Добавляем кнопку "Назад"
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_main"),
	))

	keyboard = tgbotapi.NewInlineKeyboardMarkup(buttons...)

	message := "Выберите контейнер для управления:"
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ReplyMarkup = keyboard

	h.bot.Send(msg)
}

// handleReboot обрабатывает команду /reboot
func (h *CommandHandler) handleReboot(update tgbotapi.Update) {
	// Создание inline клавиатуры с подтверждением
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, перезагрузить", "confirm_reboot"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "back_to_main"),
		),
	)

	message := "⚠️ Вы уверены, что хотите перезагрузить сервер?"
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ReplyMarkup = keyboard

	h.bot.Send(msg)
}

// handleShutdown обрабатывает команду /shutdown
func (h *CommandHandler) handleShutdown(update tgbotapi.Update) {
	// Создание inline клавиатуры с подтверждением
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, выключить", "confirm_shutdown"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "back_to_main"),
		),
	)

	message := "⚠️ Вы уверены, что хотите выключить сервер?"
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ReplyMarkup = keyboard

	h.bot.Send(msg)
}

// handleCallbackQuery обрабатывает callback-запросы
func (h *CommandHandler) handleCallbackQuery(update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	// Отправляем пустой ответ на callback-запрос, чтобы убрать "крутилку"
	callbackResponse := tgbotapi.NewCallback(callback.ID, "")
	h.bot.AnswerCallbackQuery(callbackResponse)

	// Обработка данных callback запроса
	// Навигация по меню:
	// 1. Главное меню -> подменю -> действия -> Назад (возврат на уровень выше)
	// 2. Кнопка "Назад" всегда возвращает на один уровень вверх в иерархии меню
	if data == "amnezia_vpn" {
		// Показываем меню управления Amnezia VPN
		h.handleAmneziaVPNMenu(callback)
	} else if strings.HasPrefix(data, "container:") {
		// Получение ID контейнера
		containerID := strings.TrimPrefix(data, "container:")

		// Создание inline клавиатуры с действиями для контейнера
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔄 Restart", "restart:"+containerID),
				tgbotapi.NewInlineKeyboardButtonData("🟥 Stop", "stop:"+containerID),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🟩 Start", "start:"+containerID),
				tgbotapi.NewInlineKeyboardButtonData("📊 Status", "status:"+containerID),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📝 Logs", "logs:"+containerID),
				tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "back"),
			),
		)

		// Отправка сообщения с клавиатурой действий
		message := "Выберите действие для контейнера:"
		msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
		msg.ReplyMarkup = keyboard

		h.bot.Send(msg)
	} else if data == "back" {
		// Возврат к списку контейнеров
		// Создаем фиктивный update для повторного вызова handleContainers
		fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "/containers")
		h.handleContainers(fakeUpdate)
	} else if strings.HasPrefix(data, "restart:") {
		// Перезапуск контейнера
		containerID := strings.TrimPrefix(data, "restart:")
		h.handleContainerAction(callback, "restart", containerID)
	} else if strings.HasPrefix(data, "stop:") {
		// Остановка контейнера
		containerID := strings.TrimPrefix(data, "stop:")
		h.handleContainerAction(callback, "stop", containerID)
	} else if strings.HasPrefix(data, "start:") {
		// Запуск контейнера
		containerID := strings.TrimPrefix(data, "start:")
		h.handleContainerAction(callback, "start", containerID)
	} else if strings.HasPrefix(data, "status:") {
		// Получение статуса контейнера
		containerID := strings.TrimPrefix(data, "status:")
		h.handleContainerAction(callback, "status", containerID)
	} else if strings.HasPrefix(data, "logs:") {
		// Получение логов контейнера
		containerID := strings.TrimPrefix(data, "logs:")
		h.handleContainerAction(callback, "logs", containerID)
	} else if data == "back" {
		// Возврат к списку контейнеров
		// Создаем фиктивный update для повторного вызова handleContainers
		fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "/containers")
		h.handleContainers(fakeUpdate)
	} else if data == "containers" {
		// Создаем фиктивный update для вызова handleContainers
		fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "")
		h.handleContainers(fakeUpdate)
	} else if data == "services" {
		// Показываем список сервисов
		h.handleServices(callback)
	} else if strings.HasPrefix(data, "service:") {
		// Получение имени сервиса
		serviceName := strings.TrimPrefix(data, "service:")
		
		// Получение статуса сервиса
		serviceStatus, err := h.systemService.GetServiceStatus(serviceName)
		if err != nil {
			message := fmt.Sprintf("❌ Ошибка получения статуса сервиса %s: %v", serviceName, err)
			msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
			h.bot.Send(msg)
			return
		}
		
		// Создание inline клавиатуры с действиями для сервиса в зависимости от его статуса
		var keyboard tgbotapi.InlineKeyboardMarkup
		if serviceStatus.Status == "active" {
			// Для активных сервисов показываем Restart и Stop
			keyboard = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🔄 Restart", "restart_service:"+serviceName),
					tgbotapi.NewInlineKeyboardButtonData("🟥 Stop", "stop_service:"+serviceName),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("📊 Status", "status_service:"+serviceName),
					tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "services"),
				),
			)
		} else {
			// Для неактивных сервисов показываем Start
			keyboard = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🟩 Start", "start_service:"+serviceName),
					tgbotapi.NewInlineKeyboardButtonData("🔄 Restart", "restart_service:"+serviceName),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("📊 Status", "status_service:"+serviceName),
					tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "services"),
				),
			)
		}
		
		// Отправка сообщения с клавиатурой действий
		message := fmt.Sprintf("Выберите действие для сервиса *%s* (%s):", serviceName, serviceStatus.Status)
		msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		
		h.bot.Send(msg)
	} else if strings.HasPrefix(data, "restart_service:") {
		// Перезапуск сервиса
		serviceName := strings.TrimPrefix(data, "restart_service:")
		h.handleServiceAction(callback, "restart", serviceName)
	} else if strings.HasPrefix(data, "stop_service:") {
		// Остановка сервиса
		serviceName := strings.TrimPrefix(data, "stop_service:")
		h.handleServiceAction(callback, "stop", serviceName)
	} else if strings.HasPrefix(data, "start_service:") {
		// Запуск сервиса
		serviceName := strings.TrimPrefix(data, "start_service:")
		h.handleServiceAction(callback, "start", serviceName)
	} else if strings.HasPrefix(data, "status_service:") {
		// Получение статуса сервиса
		serviceName := strings.TrimPrefix(data, "status_service:")
		h.handleServiceAction(callback, "status", serviceName)
	} else {
		// Обработка остальных callback-запросов
		switch data {
		case "status":
			// Создаем фиктивный update для вызова handleStatus
			fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "")
			h.handleStatus(fakeUpdate)
		case "containers":
			// Создаем фиктивный update для вызова handleContainers
			fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "")
			h.handleContainers(fakeUpdate)
		case "amnezia_vpn":
			// Показываем меню управления Amnezia VPN
			h.handleAmneziaVPNMenu(callback)
		case "services":
			// Показываем список сервисов
			h.handleServices(callback)
		case "services_active":
			// Показываем список активных сервисов
			h.handleServicesActive(callback)
		case "services_inactive":
			// Показываем список неактивных сервисов
			h.handleServicesInactive(callback)
		case "server_management":
			// Показываем меню управления сервером
			h.handleServerManagement(callback)
		case "reboot":
			// Создаем фиктивный update для вызова handleReboot
			fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "")
			h.handleReboot(fakeUpdate)
		case "shutdown":
			// Создаем фиктивный update для вызова handleShutdown
			fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "")
			h.handleShutdown(fakeUpdate)
		case "back_to_main":
			// Создаем фиктивный update для вызова handleStart
			fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "")
			h.handleStart(fakeUpdate)
		case "confirm_reboot":
			// Выполняем перезагрузку сервера
			err := h.systemService.Reboot()
			if err != nil {
				message := fmt.Sprintf("❌ Ошибка перезагрузки сервера: %v", err)
				msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
				h.bot.Send(msg)
			} else {
				message := "🔄 Сервер перезагружается..."
				msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
				h.bot.Send(msg)
			}
		case "confirm_shutdown":
			// Выполняем выключение сервера
			err := h.systemService.Shutdown()
			if err != nil {
				message := fmt.Sprintf("❌ Ошибка выключения сервера: %v", err)
				msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
				h.bot.Send(msg)
			} else {
				message := "🔌 Сервер выключается..."
				msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
				h.bot.Send(msg)
			}
		case "check_updates":
			// Проверка обновлений системы
			message := "🔍 Проверяю доступные обновления..."
			msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
			h.bot.Send(msg)

			// Выполняем проверку обновлений
			updates, err := h.systemService.CheckUpdates()
			if err != nil {
				message = fmt.Sprintf("❌ Ошибка проверки обновлений: %v", err)
				msg = tgbotapi.NewMessage(callback.Message.Chat.ID, message)
				h.bot.Send(msg)
			} else {
				// Проверяем, есть ли реальные пакеты для обновления
				lines := strings.Split(updates, "\n")
				packageCount := 0

				// Подсчитываем количество строк с реальными пакетами (исключая заголовок и пустые строки)
				for _, line := range lines {
					if strings.Contains(line, "/") && !strings.HasPrefix(line, "Listing...") {
						packageCount++
					}
				}

				if packageCount == 0 {
					// Если обновлений нет, выводим сообщение и возвращаем к основному меню
					message = "✅ Все обновления установлены!"
					msg = tgbotapi.NewMessage(callback.Message.Chat.ID, message)
					h.bot.Send(msg)

					// Создаем фиктивный update для возврата к основному меню
					fakeUpdate := h.createFakeUpdate(callback.Message.Chat, "/start")
					h.handleStart(fakeUpdate)
				} else {
					// Если есть обновления, показываем их
					// Ограничиваем вывод до 2000 символов
					if len(updates) > 2000 {
						updates = updates[:2000] + "\n... (вывод обрезан)"
					}
					message = fmt.Sprintf("🔍 *Доступные обновления:*\n```\n%s\n```", updates)
					msg = tgbotapi.NewMessage(callback.Message.Chat.ID, message)
					msg.ParseMode = "Markdown"
					h.bot.Send(msg)
				}
			}
		case "upgrade_system":
			// Обновление системы
			message := "⬆️ Начинаю обновление системы..."
			msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
			h.bot.Send(msg)

			// Выполняем обновление системы
			err := h.systemService.UpgradeSystem()
			if err != nil {
				message = fmt.Sprintf("❌ Ошибка обновления системы: %v", err)
			} else {
				message = "✅ Система успешно обновлена!"
			}

			msg = tgbotapi.NewMessage(callback.Message.Chat.ID, message)
			h.bot.Send(msg)
		case "list_wireguard_clients":
			// Показываем список клиентов WireGuard
			h.showWireGuardClientsList(callback)
		case "wireguard_status":
			// Показываем статус WireGuard
			h.showWireGuardStatus(callback)
		case "create_wireguard_client":
			// Создаем нового клиента WireGuard
			h.handleCreateWireGuardClient(callback)
		}
	}
	
}

// handleServerManagement показывает меню управления сервером
func (h *CommandHandler) handleServerManagement(callback *tgbotapi.CallbackQuery) {
	// Создание inline клавиатуры с командами управления сервером
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Перезагрузить сервер", "reboot"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔌 Выключить сервер", "shutdown"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔍 Проверить обновления", "check_updates"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬆️ Обновить систему", "upgrade_system"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_main"),
		),
	)

	message := "🖥 *Управление сервером*\n\nВыберите действие:"
	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)
}

// handleServices показывает список systemd сервисов с цветовыми индикаторами
func (h *CommandHandler) handleServices(callback *tgbotapi.CallbackQuery) {
	// Получение списка сервисов
	services, err := h.systemService.GetServices()
	if err != nil {
		message := fmt.Sprintf("❌ Ошибка получения списка сервисов: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, message)
		h.bot.Send(editMsg)
		return
	}

	// Разделяем сервисы на активные и неактивные
	var activeServices, inactiveServices []system.ServiceStatus
	for _, service := range services {
		if service.Status == "active" {
			activeServices = append(activeServices, service)
		} else {
			inactiveServices = append(inactiveServices, service)
		}
	}

	// Создание inline клавиатуры с подменю
	var keyboard tgbotapi.InlineKeyboardMarkup
	buttons := make([][]tgbotapi.InlineKeyboardButton, 0)

	// Добавляем кнопки для подменю
	if len(activeServices) > 0 {
		button := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("✅ Активные (%d)", len(activeServices)),
			"services_active",
		)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(button))
	}

	if len(inactiveServices) > 0 {
		button := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("❌ Неактивные (%d)", len(inactiveServices)),
			"services_inactive",
		)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(button))
	}

	// Добавляем кнопку "Назад"
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_main"),
	))

	keyboard = tgbotapi.NewInlineKeyboardMarkup(buttons...)

	message := "⚙️ *Сервисы системы*\n\nВыберите категорию сервисов:"
	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)
}

// showWireGuardClientsList показывает список клиентов WireGuard, полученных напрямую из контейнера
func (h *CommandHandler) showWireGuardClientsList(callback *tgbotapi.CallbackQuery) {
	// Получение списка клиентов WireGuard напрямую из контейнера
	clients, err := h.amneziaService.GetWireGuardClients()
	if err != nil {
		message := fmt.Sprintf("❌ Ошибка получения списка клиентов WireGuard: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, message)
		h.bot.Send(editMsg)
		return
	}
	
	if len(clients) == 0 {
		message := "📭 Нет клиентов WireGuard"
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, message)
		h.bot.Send(editMsg)
		return
	}
	
	// Формирование сообщения со списком клиентов в новом формате
	message := "Список клиентов WireGuard:\n"
	for _, client := range clients {
		// Используем метод String() структуры WireGuardClient для форматирования
		message += client.String() + "\n"
	}
	
	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, h.createBackKeyboard())
}

// handleAmneziaVPNMenu показывает меню управления Amnezia VPN
func (h *CommandHandler) handleAmneziaVPNMenu(callback *tgbotapi.CallbackQuery) {
	// Создание inline клавиатуры с командами управления Amnezia VPN
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Список WireGuard клиентов", "list_wireguard_clients"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус WireGuard", "wireguard_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Создать клиента WireGuard", "create_wireguard_client"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_main"),
		),
	)
	
	message := "🛡️ *Управление Amnezia VPN*\n\nВыберите действие:"
	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)
}

// handleServicesActive показывает список активных systemd сервисов
func (h *CommandHandler) handleServicesActive(callback *tgbotapi.CallbackQuery) {
	h.showServicesList(callback, true)
}

// handleServicesInactive показывает список неактивных systemd сервисов
func (h *CommandHandler) handleServicesInactive(callback *tgbotapi.CallbackQuery) {
	h.showServicesList(callback, false)
}

// createBackKeyboard создает клавиатуру с кнопкой "Назад"
func (h *CommandHandler) createBackKeyboard() *tgbotapi.InlineKeyboardMarkup {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_main"),
		),
	)
	return &keyboard
}

// handleContainerAction обрабатывает действия с контейнером
func (h *CommandHandler) handleContainerAction(callback *tgbotapi.CallbackQuery, action, containerID string) {
	var message string
	var err error

	switch action {
	case "restart":
		err = h.dockerService.RestartContainer(containerID)
		if err != nil {
			message = "❌ Ошибка перезапуска контейнера"
		} else {
			message = "✅ Контейнер успешно перезапущен"
		}
	case "stop":
		err = h.dockerService.StopContainer(containerID)
		if err != nil {
			message = "❌ Ошибка остановки контейнера"
		} else {
			message = "✅ Контейнер успешно остановлен"
		}
	case "start":
		err = h.dockerService.StartContainer(containerID)
		if err != nil {
			message = "❌ Ошибка запуска контейнера"
		} else {
			message = "✅ Контейнер успешно запущен"
		}
	case "status":
		status, err := h.dockerService.GetContainerStatus(containerID)
		if err != nil {
			message = fmt.Sprintf("❌ Ошибка получения статуса контейнера: %v", err)
		} else {
			message = fmt.Sprintf("Статус контейнера *%s*:\n```\n%s\n```", containerID[:12], status)
		}
	case "logs":
		logs, err := h.dockerService.GetContainerLogs(containerID, 100)
		if err != nil {
			message = "❌ Ошибка получения логов контейнера"
		} else {
			message = logs
		}
	}

	// Отправляем ответ пользователю
	h.sendActionResponse(callback.Message.Chat.ID, message, action == "status" || action == "logs")
}

// handleServiceAction обрабатывает действия с сервисом
func (h *CommandHandler) handleServiceAction(callback *tgbotapi.CallbackQuery, action, serviceName string) {
	var message string

	switch action {
	case "restart":
		serviceStatus, err := h.systemService.GetServiceStatus(serviceName)
		if err != nil {
			message = fmt.Sprintf("❌ Ошибка получения статуса сервиса %s: %v", serviceName, err)
		} else {
			if serviceStatus.Status == "active" {
				// Сервис активен, можно перезапускать
				cmd := exec.Command("sudo", "systemctl", "restart", serviceName+".service")
				err := cmd.Run()
				if err != nil {
					message = fmt.Sprintf("❌ Ошибка перезапуска сервиса %s: %v", serviceName, err)
				} else {
					message = fmt.Sprintf("✅ Сервис %s успешно перезапущен", serviceName)
				}
			} else {
				// Сервис не активен, пытаемся запустить
				cmd := exec.Command("sudo", "systemctl", "start", serviceName+".service")
				err := cmd.Run()
				if err != nil {
					message = fmt.Sprintf("❌ Ошибка запуска сервиса %s: %v", serviceName, err)
				} else {
					message = fmt.Sprintf("✅ Сервис %s успешно запущен", serviceName)
				}
			}
		}
	case "stop":
		cmd := exec.Command("sudo", "systemctl", "stop", serviceName+".service")
		err := cmd.Run()
		if err != nil {
			message = fmt.Sprintf("❌ Ошибка остановки сервиса %s: %v", serviceName, err)
		} else {
			message = fmt.Sprintf("✅ Сервис %s успешно остановлен", serviceName)
		}
	case "start":
		cmd := exec.Command("sudo", "systemctl", "start", serviceName+".service")
		err := cmd.Run()
		if err != nil {
			message = fmt.Sprintf("❌ Ошибка запуска сервиса %s: %v", serviceName, err)
		} else {
			message = fmt.Sprintf("✅ Сервис %s успешно запущен", serviceName)
		}
	case "status":
		cmd := exec.Command("sudo", "systemctl", "status", serviceName+".service")
		output, err := cmd.Output()
		if err != nil {
			message = fmt.Sprintf("❌ Ошибка получения статуса сервиса %s: %v", serviceName, err)
		} else {
			// Ограничиваем вывод до 1000 символов
			outputStr := string(output)
			if len(outputStr) > 1000 {
				outputStr = outputStr[:1000] + "\n... (вывод обрезан)"
			}
			message = fmt.Sprintf("Статус сервиса *%s*:\n```\n%s\n```", serviceName, outputStr)
		}
	}

	// Отправляем ответ пользователю
	h.sendActionResponse(callback.Message.Chat.ID, message, action == "status")
}

// handleUnknown обрабатывает неизвестные команды
func (h *CommandHandler) handleUnknown(update tgbotapi.Update) {
	message := "❓ Неизвестная команда. Используйте /start для получения списка доступных команд."

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	h.bot.Send(msg)
}

// createFakeUpdate создает фиктивный update для вызова обработчиков
func (h *CommandHandler) createFakeUpdate(chat *tgbotapi.Chat, text string) tgbotapi.Update {
	return tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: chat,
			Text: text,
		},
	}
}

// getFilteredAndSortedServices получает список сервисов, фильтрует их по статусу и сортирует по имени
func (h *CommandHandler) getFilteredAndSortedServices(active bool) ([]system.ServiceStatus, error) {
	// Получение списка сервисов
	services, err := h.systemService.GetServices()
	if err != nil {
		return nil, err
	}

	// Фильтруем сервисы по статусу
	var filteredServices []system.ServiceStatus
	for _, service := range services {
		if (active && service.Status == "active") || (!active && service.Status != "active") {
			filteredServices = append(filteredServices, service)
		}
	}

	// Сортируем сервисы по имени в алфавитном порядке
	sort.Slice(filteredServices, func(i, j int) bool {
		return filteredServices[i].Name < filteredServices[j].Name
	})

	return filteredServices, nil
}

// showServicesList показывает список сервисов с заданным статусом
func (h *CommandHandler) showServicesList(callback *tgbotapi.CallbackQuery, active bool) {
	// Получение отфильтрованного и отсортированного списка сервисов
	services, err := h.getFilteredAndSortedServices(active)
	if err != nil {
		message := fmt.Sprintf("❌ Ошибка получения списка сервисов: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, message)
		h.bot.Send(editMsg)
		return
	}

	// Создание inline клавиатуры с сервисами
	var keyboard tgbotapi.InlineKeyboardMarkup
	buttons := make([][]tgbotapi.InlineKeyboardButton, 0)

	// Добавляем кнопки для каждого сервиса (ограничим до 30 для удобства)
	limit := len(services)
	if limit > 30 {
		limit = 30
	}

	for i := 0; i < limit; i++ {
		service := services[i]
		button := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s %s", service.Emoji, service.Name), // Используем эмодзи и имя сервиса
			fmt.Sprintf("service:%s", service.Name),           // Используем имя для callback
		)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(button))
	}

	// Добавляем кнопку "Назад"
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "services"),
	))

	keyboard = tgbotapi.NewInlineKeyboardMarkup(buttons...)

	// Формируем сообщение в зависимости от статуса сервисов
	var message string
	if active {
		message = fmt.Sprintf("✅ *Активные сервисы* (%d)\n\nВыберите сервис для управления:", len(services))
	} else {
		message = fmt.Sprintf("❌ *Неактивные сервисы* (%d)\n\nВыберите сервис для управления:", len(services))
	}

	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)
}

// sendSimpleMessage отправляет простое сообщение с клавиатурой "Назад"
func (h *CommandHandler) sendSimpleMessage(chatID int64, message string) {
	h.sendSimpleMessageWithKeyboard(chatID, message, h.createBackKeyboard())
}

// sendSimpleMessageWithKeyboard отправляет простое сообщение с заданной клавиатурой
func (h *CommandHandler) sendSimpleMessageWithKeyboard(chatID int64, message string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.bot.Send(msg)
}

// sendEditMessageWithKeyboard отправляет редактируемое сообщение с заданной клавиатурой
func (h *CommandHandler) sendEditMessageWithKeyboard(chatID int64, messageID int, message string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, message)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = keyboard
	h.bot.Send(editMsg)
}

// sendActionResponse отправляет ответ на действие пользователя
func (h *CommandHandler) sendActionResponse(chatID int64, message string, parseMode bool) {
	msg := tgbotapi.NewMessage(chatID, message)
	if parseMode {
		msg.ParseMode = "Markdown"
	}
	h.bot.Send(msg)
}

// handleCreateWireGuardClientInput обрабатывает ввод имени клиента для создания WireGuard клиента
func (h *CommandHandler) handleCreateWireGuardClientInput(update tgbotapi.Update, pendingInput PendingInput) {
	// Получаем имя клиента из сообщения пользователя
	clientName := strings.TrimSpace(update.Message.Text)
	chatID := update.Message.Chat.ID
	
	// Удаляем pending input
	delete(h.pendingInputs, chatID)
	
	// Проверяем, что имя клиента не пустое
	if clientName == "" {
		message := "❌ Имя клиента не может быть пустым. Попробуйте еще раз."
		msg := tgbotapi.NewMessage(chatID, message)
		h.bot.Send(msg)
		return
	}
	
	// Создаем клиента WireGuard
	_, amneziaVPNConfig, amneziaWGConfig, err := h.amneziaService.CreateWireGuardClient(clientName)
	if err != nil {
		message := fmt.Sprintf("❌ Ошибка создания клиента WireGuard: %v", err)
		msg := tgbotapi.NewMessage(chatID, message)
		h.bot.Send(msg)
		return
	}
	
	// Отправляем подтверждение создания клиента
	message := fmt.Sprintf("✅ Клиент WireGuard *%s* успешно создан!", clientName)
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	h.bot.Send(msg)
	
	// Отправляем конфигурацию Amnezia VPN в виде блока кода
	message = fmt.Sprintf("Конфигурация Amnezia VPN:\n```\n%s\n```", amneziaVPNConfig)
	msg = tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	h.bot.Send(msg)
	
	// Отправляем файл конфигурации в формате AmneziaWG
	amneziaWGFileName := fmt.Sprintf("%s_amnezia_wg.conf", clientName)
	doc2 := tgbotapi.NewDocumentUpload(chatID, tgbotapi.FileBytes{
		Name:  amneziaWGFileName,
		Bytes: []byte(amneziaWGConfig),
	})
	h.bot.Send(doc2)
	
	// Отправляем кнопку "Назад"
	backMsg := tgbotapi.NewMessage(chatID, "Файлы конфигурации отправлены.")
	backMsg.ReplyMarkup = h.createBackKeyboard()
	h.bot.Send(backMsg)
}

// handleCreateWireGuardClient обрабатывает создание нового клиента WireGuard
func (h *CommandHandler) handleCreateWireGuardClient(callback *tgbotapi.CallbackQuery) {
	// Запрашиваем у пользователя имя нового клиента
	message := "Введите имя нового клиента WireGuard:"
	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
	h.bot.Send(msg)
	
	// Сохраняем состояние ожидания ввода от пользователя
	h.pendingInputs[callback.Message.Chat.ID] = PendingInput{
		Action: "create_wireguard_client",
		Data:   make(map[string]string),
	}
}

// showWireGuardStatus показывает статус WireGuard интерфейса
func (h *CommandHandler) showWireGuardStatus(callback *tgbotapi.CallbackQuery) {
	// Получение статуса WireGuard
	status, err := h.amneziaService.GetWireGuardStatus()
	if err != nil {
		message := fmt.Sprintf("❌ Ошибка получения статуса WireGuard: %v", err)
		h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, h.createBackKeyboard())
		return
	}
	
	// Отправляем статус WireGuard
	message := fmt.Sprintf("Статус WireGuard:\n```\n%s\n```", status)
	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, h.createBackKeyboard())
}