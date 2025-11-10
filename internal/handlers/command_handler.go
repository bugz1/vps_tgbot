package handlers

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"tgbot/internal/cmdrunner"
	contextpkg "tgbot/internal/context"
	"tgbot/internal/password"
	"tgbot/internal/services/amnezia"
	"tgbot/internal/services/docker"
	"tgbot/internal/services/system"
	"tgbot/internal/workerpool"
	"tgbot/pkg/logger"

	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// CommandHandler обработчик команд
type CommandHandler struct {
	bot               *tgbotapi.BotAPI
	systemService     *system.Monitor
	dockerService     *docker.Manager
	amneziaService    *amnezia.Service
	workerPool        *workerpool.WorkerPool
	passwordRequester *password.Requester
	// Храним состояние ожидания ввода от пользователя
	pendingInputs map[int64]PendingInput
	// Карта для хранения временных закодированных ключей -> публичный ключ для callback
	deleteTokenToKey map[string]string
	// Карта для хранения временных закодированных ключей -> имя клиента для callback
	deleteTokenToName map[string]string
	deleteMu          sync.Mutex
	// Временные метки истечения токенов для очистки
	deleteTokenExpiry map[string]time.Time
	// Стек навигации по чатам (история экранов)
	navStack map[int64][]string
	// Текущий экран для чата
	currentView map[int64]string
}

// PendingInput структура для хранения информации о ожидаемом вводе от пользователя
type PendingInput struct {
	Action string
	Data   map[string]string
}

// NewCommandHandler создает новый обработчик команд
func NewCommandHandler(bot *tgbotapi.BotAPI, systemService *system.Monitor, dockerService *docker.Manager, amneziaService *amnezia.Service, workerPool *workerpool.WorkerPool, passwordRequester *password.Requester) *CommandHandler {
	h := &CommandHandler{
		bot:               bot,
		systemService:     systemService,
		dockerService:     dockerService,
		amneziaService:    amneziaService,
		workerPool:        workerPool,
		passwordRequester: passwordRequester,
		pendingInputs:     make(map[int64]PendingInput),
		deleteTokenToKey:  make(map[string]string),
		deleteTokenToName: make(map[string]string),
		deleteTokenExpiry: make(map[string]time.Time),
		navStack:          make(map[int64][]string),
		currentView:       make(map[int64]string),
	}

	// запуск фонового очистителя
	h.startDeleteTokenCleaner()

	return h
}

// startDeleteTokenCleaner запускает фоновую горутину, которая периодически удаляет истекшие токены
// TTL составляет 10 минут для безопасности. Эта функция идемпотентна, если вызывается несколько раз для одного и того же обработчика.
func (h *CommandHandler) startDeleteTokenCleaner() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			h.deleteMu.Lock()
			for k, ts := range h.deleteTokenExpiry {
				if now.Sub(ts) > 10*time.Minute {
					delete(h.deleteTokenExpiry, k)
					delete(h.deleteTokenToKey, k)
					delete(h.deleteTokenToName, k)
				}
			}
			h.deleteMu.Unlock()
		}
	}()
}

// encodeKeyForCallback делает безопасную для callback-данных версию publicKey
func encodeKeyForCallback(pub string) string {
	s := strings.ReplaceAll(pub, "+", "-")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.TrimRight(s, "=")
	return s
}

// decodeKeyFromCallback восстанавливает оригинальный publicKey из закодированной формы
func decodeKeyFromCallback(enc string) string {
	s := strings.ReplaceAll(enc, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	// Добавляем padding для base64
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return s
}

// HandleCommand обрабатывает команды
func (h *CommandHandler) HandleCommand(update tgbotapi.Update) {
	// Генерируем уникальный traceId для этого запроса
	traceID := contextpkg.GenerateTraceID()

	// Создаем контекст с traceId
	ctx := contextpkg.WithTraceID(context.Background(), traceID)

	// Логируем начало обработки команды с traceId
	logger.LogWithCtx(ctx, logger.Info, "handlers.command.start", map[string]interface{}{
		"update_id":    update.UpdateID,
		"has_message":  update.Message != nil,
		"has_callback": update.CallbackQuery != nil,
	})

	if update.Message != nil {
		// Получение текста команды
		command := strings.TrimSpace(update.Message.Text)
		chatID := update.Message.Chat.ID

		// Проверяем, есть ли ожидание ввода от пользователя
		if pendingInput, exists := h.pendingInputs[chatID]; exists {
			// Обрабатываем ввод пользователя в зависимости от ожидаемого действия
			switch pendingInput.Action {
			case "create_wireguard_client":
				h.handleCreateWireGuardClientInput(ctx, update, pendingInput)
			case "remove_wireguard_client":
				h.handleRemoveWireGuardClientInput(ctx, update, pendingInput)
			default:
				// Если действие не определено, удаляем pending input и обрабатываем как обычную команду
				delete(h.pendingInputs, chatID)
				h.handleRegularCommand(ctx, update, command)
			}
			return
		}

		// Обработка обычных команд
		h.handleRegularCommand(ctx, update, command)
	} else if update.CallbackQuery != nil {
		// Обработка callback-запросов
		h.handleCallbackQuery(ctx, update)
	}

	// Логируем завершение обработки команды с traceId
	logger.LogWithCtx(ctx, logger.Info, "handlers.command.end", map[string]interface{}{
		"update_id": update.UpdateID,
	})
}

// handleRegularCommand обрабатывает обычные команды
func (h *CommandHandler) handleRegularCommand(ctx context.Context, update tgbotapi.Update, command string) {
	switch {
	case command == "/start":
		h.showMainMenu(update.Message.Chat.ID)
	case command == "/help":
		h.showHelp(update.Message.Chat.ID)
	default:
		h.handleUnknown(update)
	}
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

// showContainersMenu отображает меню контейнеров
func (h *CommandHandler) showContainersMenu(chatID int64) {
	h.showItemsMenu(chatID, 0, "container")
	h.currentView[chatID] = "containers"
}

// showRebootConfirmation отображает подтверждение перезагрузки сервера
func (h *CommandHandler) showRebootConfirmation(chatID int64) {
	h.showServerActionConfirmation(chatID, "перезагрузить", "confirm_reboot", "back_to_main")
}

// showShutdownConfirmation отображает подтверждение выключения сервера
func (h *CommandHandler) showShutdownConfirmation(chatID int64) {
	h.showServerActionConfirmation(chatID, "выключить", "confirm_shutdown", "back_to_main")
}

// showServerActionConfirmation отображает подтверждение для действий с сервером
func (h *CommandHandler) showServerActionConfirmation(chatID int64, actionText, confirmCallback, cancelCallback string) {
	keyboard := h.createConfirmationKeyboard(
		fmt.Sprintf("✅ Да, %s", actionText), confirmCallback,
		"❌ Отмена", cancelCallback,
	)
	message := fmt.Sprintf("⚠️ Вы уверены, что хотите %s сервер?", actionText)
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ReplyMarkup = keyboard
	h.bot.Send(msg)
}

// handleWireGuardDeleteCallback обрабатывает callback для удаления клиента WireGuard
func (h *CommandHandler) handleWireGuardDeleteCallback(ctx context.Context, callback *tgbotapi.CallbackQuery, data string) bool {
	if !strings.HasPrefix(data, "del_wg:") && !strings.HasPrefix(data, "confirm_del_wg:") {
		return false
	}

	if strings.HasPrefix(data, "del_wg:") {
		enc := strings.TrimPrefix(data, "del_wg:")
		// Извлекаем publicKey и имя клиента из временной карты, если есть
		h.deleteMu.Lock()
		pub, ok := h.deleteTokenToKey[enc]
		name, nOk := h.deleteTokenToName[enc]
		h.deleteMu.Unlock()
		if !ok {
			// Резервный вариант: пробуем декодировать
			pub = decodeKeyFromCallback(enc)
		}
		// Диагностический лог (с маскированием) с контекстом
		logger.LogWithCtx(ctx, logger.Debug, "handlers.del_wg_pressed", map[string]interface{}{"token": enc, "present": ok, "name": name, "pub": logger.MaskKey(pub)})
		// Показываем подтверждающую клавиатуру
		keyboard := h.createConfirmationKeyboard(
			"✅ Удалить", "confirm_del_wg:"+enc,
			"❌ Отмена", "list_wireguard_clients",
		)
		var displayText string
		if nOk && name != "" {
			displayText = fmt.Sprintf("%s (%s)", name, pub)
		} else {
			displayText = pub
		}
		msg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, fmt.Sprintf("Удалить клиента: %s ?", displayText))
		msg.ReplyMarkup = keyboard
		h.bot.Send(msg)
		return true
	}

	if strings.HasPrefix(data, "confirm_del_wg:") {
		enc := strings.TrimPrefix(data, "confirm_del_wg:")
		h.deleteMu.Lock()
		pub, ok := h.deleteTokenToKey[enc]
		h.deleteMu.Unlock()
		if !ok {
			pub = decodeKeyFromCallback(enc)
		}

		// Диагностический лог (с маскированием) с контекстом
		logger.LogWithCtx(ctx, logger.Debug, "handlers.confirm_del_wg", map[string]interface{}{"token": enc, "present": ok, "pub": logger.MaskKey(pub)})

		// Выполняем удаление
		err := h.amneziaService.RemoveWireGuardClient(pub)
		if err != nil {
			h.sendErrorToCallback(callback, "удаления клиента", err)
			return true
		}

		// Удаляем токен после успешного удаления
		h.deleteMu.Lock()
		delete(h.deleteTokenToKey, enc)
		delete(h.deleteTokenToName, enc)
		delete(h.deleteTokenExpiry, enc)
		h.deleteMu.Unlock()

		// Сообщение о перезагрузке конфигурации
		reloadMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, "Идет перезагрузка конфигурации...")
		h.bot.Send(reloadMsg)

		// Ждем 5 секунд, чтобы конфигурация точно перезагрузилась
		time.Sleep(5 * time.Second)

		// Показываем обновленный список клиентов
		h.showWireGuardClientsList(callback)
		return true
	}

	return false
}

// handleMenuNavigation обрабатывает навигацию по меню
func (h *CommandHandler) handleMenuNavigation(callback *tgbotapi.CallbackQuery, data string) bool {
	switch data {
	case "status_overview":
		// Показываем общий статус системы напрямую
		h.handleStatus(h.createFakeUpdate(callback.Message.Chat, ""))
		return true
	case CbAmneziaVPN:
		h.pushView(callback.Message.Chat.ID, h.currentView[callback.Message.Chat.ID])
		h.showAmneziaVPNMenu(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case CbContainers:
		h.pushView(callback.Message.Chat.ID, h.currentView[callback.Message.Chat.ID])
		h.showContainersMenu(callback.Message.Chat.ID)
		return true
	case CbServices:
		h.pushView(callback.Message.Chat.ID, h.currentView[callback.Message.Chat.ID])
		h.showServicesMenu(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case CbServerMgmt:
		h.pushView(callback.Message.Chat.ID, h.currentView[callback.Message.Chat.ID])
		h.showServerManagementMenu(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case "updates_management":
		// Переход в подменю управления обновлениями
		h.pushView(callback.Message.Chat.ID, "server_management")
		h.showUpdatesMenu(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case "power_management":
		// Переход в подменю управления питанием
		h.pushView(callback.Message.Chat.ID, "server_management")
		h.showPowerMenu(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case CbBack:
		h.navigateBack(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case CbNoop:
		// Пустое действие (используется для индикатора пагинации)
		return true
	case "back_to_main":
		h.showMainMenu(callback.Message.Chat.ID)
		return true
	case "back":
		h.showContainersMenu(callback.Message.Chat.ID)
		return true
	}
	return false
}

// handleContainerCallbacks обрабатывает callback-запросы для контейнеров
func (h *CommandHandler) handleContainerCallbacks(callback *tgbotapi.CallbackQuery, data string) bool {
	if strings.HasPrefix(data, "container:") {
		containerID := strings.TrimPrefix(data, "container:")
		// Получаем статус контейнера, чтобы показать релевантные действия
		contStatus, _ := h.dockerService.GetContainerStatus(containerID)
		isRunning := strings.Contains(strings.ToLower(contStatus), "up") || strings.Contains(strings.ToLower(contStatus), "running")

		var rows [][]tgbotapi.InlineKeyboardButton
		if isRunning {
			// Для запущенных: Restart, Stop
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔄 Restart", "restart:"+containerID),
				tgbotapi.NewInlineKeyboardButtonData("🟥 Stop", "stop:"+containerID),
			))
		} else {
			// Для остановленных: только Start
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🟩 Start", "start:"+containerID),
			))
		}
		// Общие кнопки
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Status", "status:"+containerID),
			tgbotapi.NewInlineKeyboardButtonData("📝 Logs", "logs:"+containerID),
		))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "nav_back"),
		))
		keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
		message := "Выберите действие для контейнера:"
		h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)
		return true
	}

	if strings.HasPrefix(data, "containers_page:") {
		// containers_page:<num>
		parts := strings.Split(strings.TrimPrefix(data, "containers_page:"), ":")
		pageStr := parts[0]
		page := 0
		if p, err := strconv.Atoi(pageStr); err == nil && p >= 0 {
			page = p
		}
		h.showContainersPage(callback.Message.Chat.ID, callback.Message.MessageID, page)
		return true
	}

	if strings.HasPrefix(data, "restart:") || strings.HasPrefix(data, "stop:") ||
		strings.HasPrefix(data, "start:") || strings.HasPrefix(data, "status:") ||
		strings.HasPrefix(data, "logs:") {
		action := strings.Split(data, ":")[0]
		containerID := strings.TrimPrefix(data, action+":")
		h.handleItemAction(callback, action, containerID, "container")
		return true
	}

	return false
}

// handleServiceCallbacks обрабатывает callback-запросы для сервисов
func (h *CommandHandler) handleServiceCallbacks(callback *tgbotapi.CallbackQuery, data string) bool {
	if strings.HasPrefix(data, "service:") {
		serviceName := strings.TrimPrefix(data, "service:")
		serviceStatus, err := h.systemService.GetServiceStatus(serviceName)
		if err != nil {
			h.sendErrorToCallback(callback, fmt.Sprintf("получения статуса сервиса %s", serviceName), err)
			return true
		}

		keyboard := h.createServiceKeyboard(serviceName, serviceStatus.Status == "active")
		safeName := escapeMarkdownBasic(serviceName)
		safeStatus := escapeMarkdownBasic(serviceStatus.Status)
		message := fmt.Sprintf("Выберите действие для сервиса *%s* (%s):", safeName, safeStatus)
		h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, keyboard)
		return true
	}

	if strings.HasPrefix(data, "restart_service:") || strings.HasPrefix(data, "stop_service:") ||
		strings.HasPrefix(data, "start_service:") || strings.HasPrefix(data, "status_service:") {
		parts := strings.Split(data, ":")
		if len(parts) == 2 {
			action := strings.TrimSuffix(parts[0], "_service")
			h.handleItemAction(callback, action, parts[1], "service")
			return true
		}
	}

	if strings.HasPrefix(data, "services_active_page:") || strings.HasPrefix(data, "services_inactive_page:") {
		active := strings.HasPrefix(data, "services_active_page:")
		pageStr := strings.TrimPrefix(strings.TrimPrefix(data, "services_active_page:"), "services_inactive_page:")
		page := 0
		if p, err := strconv.Atoi(pageStr); err == nil && p >= 0 {
			page = p
		}
		h.showServicesListPaged(callback.Message.Chat.ID, callback.Message.MessageID, active, page)
		return true
	}

	return false
}

// handleSystemCallbacks обрабатывает callback-запросы для системных операций
func (h *CommandHandler) handleSystemCallbacks(callback *tgbotapi.CallbackQuery, data string) bool {
	switch data {
	case "status", "cpu", "ram", "hdd":
		// Все запросы статуса показывают общий статус системы
		h.handleStatus(h.createFakeUpdate(callback.Message.Chat, ""))
		return true
	case "services_active":
		h.showServicesListWithStatus(callback.Message.Chat.ID, callback.Message.MessageID, true)
		return true
	case "services_inactive":
		h.showServicesListWithStatus(callback.Message.Chat.ID, callback.Message.MessageID, false)
		return true
	case "reboot":
		h.showRebootConfirmation(callback.Message.Chat.ID)
		return true
	case "shutdown":
		h.showShutdownConfirmation(callback.Message.Chat.ID)
		return true
	case "confirm_reboot":
		err := h.systemService.Reboot()
		if err != nil {
			h.sendError(callback.Message.Chat.ID, "перезагрузки сервера", err)
		} else {
			h.sendActionResponse(callback.Message.Chat.ID, "🔄 Сервер перезагружается...", false)
		}
		return true
	case "confirm_shutdown":
		err := h.systemService.Shutdown()
		if err != nil {
			h.sendError(callback.Message.Chat.ID, "выключения сервера", err)
		} else {
			h.sendActionResponse(callback.Message.Chat.ID, "🔌 Сервер выключается...", false)
		}
		return true
	case "check_updates":
		h.handleCheckUpdates(callback)
		return true
	case "upgrade_system":
		h.handleUpgradeSystem(callback)
		return true
	}
	return false
}

// handleWireGuardCallbacks обрабатывает callback-запросы для WireGuard
func (h *CommandHandler) handleWireGuardCallbacks(callback *tgbotapi.CallbackQuery, data string) bool {
	switch data {
	case "list_wireguard_clients":
		h.showWireGuardClientsList(callback)
		return true
	case "client_mgmt":
		// Переход в подменю управления клиентами
		h.pushView(callback.Message.Chat.ID, "amnezia_vpn")
		h.showAmneziaClientMgmtMenu(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case "vpn_mgmt":
		// Переход в подменю управления VPN (статус/бэкап/откат)
		h.pushView(callback.Message.Chat.ID, "amnezia_vpn")
		h.showAmneziaMgmtMenu(callback.Message.Chat.ID, callback.Message.MessageID)
		return true
	case "wireguard_status":
		h.showWireGuardStatus(callback)
		return true
	case "create_wireguard_client":
		h.handleCreateWireGuardClient(callback)
		return true
	case "remove_wireguard_client":
		h.handleRemoveWireGuardClient(callback)
		return true
	case "backup_configs":
		h.handleBackupConfigs(callback)
		return true
	case "rollback_configs":
		h.handleRollbackConfigs(callback)
		return true
	}

	if strings.HasPrefix(data, "wg_clients_page:") {
		pageStr := strings.TrimPrefix(data, "wg_clients_page:")
		page := 0
		if p, err := strconv.Atoi(pageStr); err == nil && p >= 0 {
			page = p
		}
		h.showWireGuardClientsListPaged(callback.Message.Chat.ID, callback.Message.MessageID, page)
		return true
	}
	return false
}

// handleCheckUpdates обрабатывает проверку обновлений системы
func (h *CommandHandler) handleCheckUpdates(callback *tgbotapi.CallbackQuery) {
	message := "🔍 Проверяю доступные обновления..."
	h.sendActionResponse(callback.Message.Chat.ID, message, false)

	updates, err := h.systemService.CheckUpdates()
	if err != nil {
		h.sendError(callback.Message.Chat.ID, "проверки обновлений", err)
		return
	}

	lines := strings.Split(updates, "\n")
	packageCount := 0
	for _, line := range lines {
		if strings.Contains(line, "/") && !strings.HasPrefix(line, "Listing...") {
			packageCount++
		}
	}

	if packageCount == 0 {
		h.sendActionResponse(callback.Message.Chat.ID, "✅ Все обновления установлены!", false)
		h.showMainMenu(callback.Message.Chat.ID)
	} else {
		if len(updates) > 2000 {
			updates = updates[:2000] + "\n... (вывод обрезан)"
		}
		message := fmt.Sprintf("🔍 *Доступные обновления:*\n```\n%s\n```", updates)
		h.sendActionResponse(callback.Message.Chat.ID, message, true)
	}
}

// handleUpgradeSystem обрабатывает обновление системы
func (h *CommandHandler) handleUpgradeSystem(callback *tgbotapi.CallbackQuery) {
	message := "⬆️ Начинаю обновление системы..."
	h.sendActionResponse(callback.Message.Chat.ID, message, false)

	err := h.systemService.UpgradeSystem()
	if err != nil {
		h.sendError(callback.Message.Chat.ID, "обновления системы", err)
	} else {
		h.sendActionResponse(callback.Message.Chat.ID, "✅ Система успешно обновлена!", false)
	}
}

// handleCallbackQuery обрабатывает callback-запросы
func (h *CommandHandler) handleCallbackQuery(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	// Отправляем пустой ответ на callback-запрос, чтобы убрать индикатор загрузки
	callbackResponse := tgbotapi.NewCallback(callback.ID, "")
	h.bot.AnswerCallbackQuery(callbackResponse)

	// Обработка callback-запросов через специализированные функции
	// Порядок важен: сначала специфичные префиксы, затем общие меню
	if h.handleWireGuardDeleteCallback(ctx, callback, data) {
		return
	}

	if h.handleMenuNavigation(callback, data) {
		return
	}

	if h.handleContainerCallbacks(callback, data) {
		return
	}

	if h.handleServiceCallbacks(callback, data) {
		return
	}

	if h.handleSystemCallbacks(callback, data) {
		return
	}

	if h.handleWireGuardCallbacks(callback, data) {
		return
	}
}

// showServerManagementMenu отображает меню управления сервером
func (h *CommandHandler) showServerManagementMenu(chatID int64, messageID int) {
	// Подменю: управление обновлениями и управление питанием
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬆️ Управление обновлениями", "updates_management"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔌 Управление питанием", "power_management"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_main"),
		),
	)

	message := "🖥 *Управление сервером*\n\nВыберите раздел:"
	h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
	h.currentView[chatID] = "server_management"
}

// showUpdatesMenu отображает подменю управления обновлениями
func (h *CommandHandler) showUpdatesMenu(chatID int64, messageID int) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔍 Проверить обновления", "check_updates"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬆️ Обновить систему", "upgrade_system"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "server_management"),
		),
	)

	message := "⬆️ *Управление обновлениями*\n\nВыберите действие:"
	h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
	h.currentView[chatID] = "updates_management"
}

// showPowerMenu отображает подменю управления питанием
func (h *CommandHandler) showPowerMenu(chatID int64, messageID int) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Перезагрузить сервер", "reboot"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔌 Выключить сервер", "shutdown"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "server_management"),
		),
	)

	message := "🔌 *Управление питанием*\n\nВыберите действие:"
	h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
	h.currentView[chatID] = "power_management"
}

// pushView добавляет экран в стек навигации для чата
func (h *CommandHandler) pushView(chatID int64, view string) {
	if view == "" {
		return
	}
	h.navStack[chatID] = append(h.navStack[chatID], view)
}

// navigateBack выполняет переход на предыдущий экран из стека
func (h *CommandHandler) navigateBack(chatID int64, messageID int) {
	stack := h.navStack[chatID]
	if len(stack) == 0 {
		h.showMainMenu(chatID)
		return
	}
	// Берем последний элемент и сокращаем стек
	prev := stack[len(stack)-1]
	h.navStack[chatID] = stack[:len(stack)-1]

	switch prev {
	case "main":
		h.showMainMenu(chatID)
	case "containers":
		h.showContainersMenu(chatID)
	case "services":
		h.showServicesMenu(chatID, messageID)
	case "server_management":
		h.showServerManagementMenu(chatID, messageID)
	case "updates_management":
		h.showUpdatesMenu(chatID, messageID)
	case "power_management":
		h.showPowerMenu(chatID, messageID)
	case "amnezia_vpn":
		h.showAmneziaVPNMenu(chatID, messageID)
	default:
		// По умолчанию возвращаемся в главное меню
		h.showMainMenu(chatID)
	}
}

// showServicesMenu отображает меню сервисов
func (h *CommandHandler) showServicesMenu(chatID int64, messageID int) {
	h.showItemsMenu(chatID, messageID, "service")
	h.currentView[chatID] = "services"
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
	logger.Log(logger.Debug, "handlers.show_wireguard_clients_list.clients", map[string]interface{}{"clients": clients})

	if len(clients) == 0 {
		message := "📭 Нет клиентов WireGuard"
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, message)
		h.bot.Send(editMsg)
		return
	}

	// Формирование сообщения со списком клиентов (только статусы)
	message := "Список клиентов WireGuard:\n"
	for _, client := range clients {
		message += client.String() + "\n"
	}
	logger.Log(logger.Debug, "handlers.show_wireguard_clients_list.message", map[string]interface{}{"message": message})

	// Кнопка назад
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav_back"),
		),
	)

	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)

}

// showAmneziaClientMgmtMenu отображает подменю управления клиентами (создание/удаление)
func (h *CommandHandler) showAmneziaClientMgmtMenu(chatID int64, messageID int) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Создать клиента WireGuard", "create_wireguard_client"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➖ Удалить клиента WireGuard", "remove_wireguard_client"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav_back"),
		),
	)
	message := "👥 *Управление клиентами*\n\nВыберите действие:"
	h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
	h.currentView[chatID] = "amnezia_client_mgmt"
}

// showAmneziaMgmtMenu отображает подменю управления (статус/бэкап/откат)
func (h *CommandHandler) showAmneziaMgmtMenu(chatID int64, messageID int) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус WireGuard", "wireguard_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💾 Резервное копирование", "backup_configs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Откат конфигурации", "rollback_configs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav_back"),
		),
	)
	message := "🛠 *Управление Amnezia*\n\nВыберите действие:"
	h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
	h.currentView[chatID] = "amnezia_mgmt"
}

// showAmneziaVPNMenu отображает меню управления Amnezia VPN
func (h *CommandHandler) showAmneziaVPNMenu(chatID int64, messageID int) {
	// Создание inline клавиатуры с командами управления Amnezia VPN
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Список клиентов", "list_wireguard_clients"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 Управление клиентами", "client_mgmt"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛠 Управление", "vpn_mgmt"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav_back"),
		),
	)

	message := "🛡️ *Amnezia VPN*\n\nВыберите раздел:"
	h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
	h.currentView[chatID] = "amnezia_vpn"
}

// showServicesList отображает список сервисов с заданным статусом
func (h *CommandHandler) showServicesListWithStatus(chatID int64, messageID int, active bool) {
	// Получение отфильтрованного и отсортированного списка сервисов
	services, err := h.getFilteredAndSortedServices(active)
	if err != nil {
		message := fmt.Sprintf("❌ Ошибка получения списка сервисов: %v", err)
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, message)
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

	h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
}

// handleItemAction обрабатывает действия с элементом (контейнером или сервисом)
func (h *CommandHandler) handleItemAction(callback *tgbotapi.CallbackQuery, action, itemName, itemType string) {
	// Нотификация пользователю, что операция выполняется
	var executingMsg tgbotapi.MessageConfig
	if itemType == "container" {
		executingMsg = tgbotapi.NewMessage(callback.Message.Chat.ID, "⏳ Выполняется операция с контейнером...")
	} else {
		executingMsg = tgbotapi.NewMessage(callback.Message.Chat.ID, "⏳ Выполняется операция со службой...")
	}
	h.bot.Send(executingMsg)

	// Создаем задачу для worker pool
	task := workerpool.Task{
		ID: fmt.Sprintf("%s_%s_%s", itemType, action, itemName),
		Handler: func() (interface{}, error) {
			var message string

			// Используем контекст с таймаутом для системных команд
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			switch itemType {
			case "container":
				switch action {
				case "restart":
					err := h.dockerService.RestartContainer(itemName)
					if err != nil {
						message = "❌ Ошибка перезапуска контейнера"
					} else {
						message = "✅ Контейнер успешно перезапущен"
					}
				case "stop":
					err := h.dockerService.StopContainer(itemName)
					if err != nil {
						message = "❌ Ошибка остановки контейнера"
					} else {
						message = "✅ Контейнер успешно остановлен"
					}
				case "start":
					err := h.dockerService.StartContainer(itemName)
					if err != nil {
						message = "❌ Ошибка запуска контейнера"
					} else {
						message = "✅ Контейнер успешно запущен"
					}
				case "status":
					status, err := h.dockerService.GetContainerStatus(itemName)
					if err != nil {
						message = fmt.Sprintf("❌ Ошибка получения статуса контейнера: %v", err)
					} else {
						message = fmt.Sprintf("Статус контейнера *%s*:\n```\n%s\n```", itemName[:12], status)
					}
				case "logs":
					logs, err := h.dockerService.GetContainerLogs(itemName, 100)
					if err != nil {
						message = "❌ Ошибка получения логов контейнера"
					} else {
						message = logs
					}
				}
			case "service":
				switch action {
				case "restart":
					serviceStatus, err := h.systemService.GetServiceStatus(itemName)
					if err != nil {
						message = fmt.Sprintf("❌ Ошибка получения статуса сервиса %s: %v", itemName, err)
						break
					}

					if serviceStatus.Status == "active" {
						// Перезапуск через cmdrunner (retries)
						parts := []string{"sudo", "systemctl", "restart", itemName + ".service"}
						opts := cmdrunner.RunOptions{
							Timeout:            30 * time.Second,
							Attempts:           3,
							PasswordFromConfig: true,
							Requester:          h.passwordRequester,
							ChatID:             callback.Message.Chat.ID,
						}
						if _, err := cmdrunner.RunWithRetries(ctx, parts, opts); err != nil {
							message = fmt.Sprintf("❌ Ошибка перезапуска сервиса %s: %v", itemName, err)
						} else {
							message = fmt.Sprintf("✅ Сервис %s успешно перезапущен", itemName)
						}
					} else {
						// Запуск через cmdrunner
						parts := []string{"sudo", "systemctl", "start", itemName + ".service"}
						opts := cmdrunner.RunOptions{
							Timeout:            30 * time.Second,
							Attempts:           3,
							PasswordFromConfig: true,
							Requester:          h.passwordRequester,
							ChatID:             callback.Message.Chat.ID,
						}
						if _, err := cmdrunner.RunWithRetries(ctx, parts, opts); err != nil {
							message = fmt.Sprintf("❌ Ошибка запуска сервиса %s: %v", itemName, err)
						} else {
							message = fmt.Sprintf("✅ Сервис %s успешно запущен", itemName)
						}
					}
				case "stop":
					parts := []string{"sudo", "systemctl", "stop", itemName + ".service"}
					opts := cmdrunner.RunOptions{
						Timeout:            30 * time.Second,
						Attempts:           3,
						PasswordFromConfig: true,
						Requester:          h.passwordRequester,
						ChatID:             callback.Message.Chat.ID,
					}
					if _, err := cmdrunner.RunWithRetries(ctx, parts, opts); err != nil {
						message = fmt.Sprintf("❌ Ошибка остановки сервиса %s: %v", itemName, err)
					} else {
						message = fmt.Sprintf("✅ Сервис %s успешно остановлен", itemName)
					}
				case "start":
					parts := []string{"sudo", "systemctl", "start", itemName + ".service"}
					opts := cmdrunner.RunOptions{
						Timeout:            30 * time.Second,
						Attempts:           3,
						PasswordFromConfig: true,
						Requester:          h.passwordRequester,
						ChatID:             callback.Message.Chat.ID,
					}
					if _, err := cmdrunner.RunWithRetries(ctx, parts, opts); err != nil {
						message = fmt.Sprintf("❌ Ошибка запуска сервиса %s: %v", itemName, err)
					} else {
						message = fmt.Sprintf("✅ Сервис %s успешно запущен", itemName)
					}
				case "status":
					parts := []string{"sudo", "systemctl", "status", itemName + ".service"}
					opts := cmdrunner.RunOptions{
						Timeout:            30 * time.Second,
						Attempts:           1,
						PasswordFromConfig: true,
						Requester:          h.passwordRequester,
						ChatID:             callback.Message.Chat.ID,
					}
					out, err := cmdrunner.RunWithRetries(ctx, parts, opts)
					if err != nil {
						message = fmt.Sprintf("❌ Ошибка получения статуса сервиса %s: %v", itemName, err)
					} else {
						outputStr := out
						if len(outputStr) > 1000 {
							outputStr = outputStr[:1000] + "\n... (вывод обрезан)"
						}
						message = fmt.Sprintf("Статус сервиса *%s*:\n```\n%s\n```", itemName, outputStr)
					}
				}
			}

			return message, nil
		},
		Timeout: 30 * time.Second,
	}

	// Отправляем задачу в worker pool
	h.workerPool.Submit(task)

	// Обрабатываем результат в отдельной goroutine
	go func() {
		h.handleWorkerPoolResult(task.ID, func(result workerpool.Result) {
			message, _ := result.Value.(string)
			isStatusOrLogs := (action == "status" || (action == "logs" && itemType == "container"))
			h.sendActionResponse(callback.Message.Chat.ID, message, isStatusOrLogs)
		})
	}()
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

// showItemsMenu отображает меню элементов (контейнеров или сервисов)
func (h *CommandHandler) showItemsMenu(chatID int64, messageID int, itemType string) {
	switch itemType {
	case "container":
		h.showContainersPage(chatID, messageID, 0)
	case "service":
		// Получение списка сервисов
		services, err := h.systemService.GetServices()
		if err != nil {
			message := fmt.Sprintf("❌ Ошибка получения списка сервисов: %v", err)
			if messageID > 0 {
				editMsg := tgbotapi.NewEditMessageText(chatID, messageID, message)
				h.bot.Send(editMsg)
			} else {
				msg := tgbotapi.NewMessage(chatID, message)
				h.bot.Send(msg)
			}
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

		// Показываем выбор категорий с переходом на страницы
		var buttons [][]tgbotapi.InlineKeyboardButton
		if len(activeServices) > 0 {
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("✅ Активные (%d)", len(activeServices)),
					"services_active_page:0",
				),
			))
		}
		if len(inactiveServices) > 0 {
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("❌ Неактивные (%d)", len(inactiveServices)),
					"services_inactive_page:0",
				),
			))
		}
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav_back"),
		))
		keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
		message := "⚙️ *Сервисы системы*\n\nВыберите категорию сервисов:"
		if messageID > 0 {
			h.sendEditMessageWithKeyboard(chatID, messageID, message, &keyboard)
		} else {
			msg := tgbotapi.NewMessage(chatID, message)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = keyboard
			h.bot.Send(msg)
		}
	}
}

// showContainersPage показывает страницу списка контейнеров
func (h *CommandHandler) showContainersPage(chatID int64, messageID int, page int) {
	containers, err := h.dockerService.ListContainers()
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка получения списка контейнеров")
		h.bot.Send(msg)
		return
	}
	total := len(containers)
	if total == 0 {
		msg := tgbotapi.NewMessage(chatID, "📭 Нет запущенных контейнеров")
		h.bot.Send(msg)
		return
	}
	page, start, end, totalPages := paginate(total, page, defaultPageSize)
	items := make([]InlineItem, 0, end-start)
	for i := start; i < end; i++ {
		c := containers[i]
		items = append(items, InlineItem{Label: fmt.Sprintf("%s [%s]", c.Name, c.Status), Callback: fmt.Sprintf("container:%s", c.ID)})
	}
	rows := makeItemRows(items)
	prevCb := fmt.Sprintf("containers_page:%d", page-1)
	nextCb := fmt.Sprintf("containers_page:%d", page+1)
	keyboard := h.createPaginationKeyboard(rows, page, totalPages, prevCb, nextCb, "nav_back")
	message := "🐳 *Контейнеры*\n\nВыберите контейнер:"
	if messageID > 0 {
		h.sendEditMessageWithKeyboard(chatID, messageID, message, keyboard)
	} else {
		msg := tgbotapi.NewMessage(chatID, message)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		h.bot.Send(msg)
	}
}

// showServicesListPaged выводит список сервисов (активные/неактивные) с пагинацией
func (h *CommandHandler) showServicesListPaged(chatID int64, messageID int, active bool, page int) {
	services, err := h.getFilteredAndSortedServices(active)
	if err != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("❌ Ошибка получения списка сервисов: %v", err))
		h.bot.Send(editMsg)
		return
	}
	total := len(services)
	if total == 0 {
		empty := "📭 Нет сервисов"
		h.sendEditMessageWithKeyboard(chatID, messageID, empty, h.createBackKeyboard())
		return
	}
	page, start, end, totalPages := paginate(total, page, defaultPageSize)
	items := make([]InlineItem, 0, end-start)
	for i := start; i < end; i++ {
		s := services[i]
		items = append(items, InlineItem{Label: fmt.Sprintf("%s %s", s.Emoji, s.Name), Callback: fmt.Sprintf("service:%s", s.Name)})
	}
	rows := makeItemRows(items)
	prefix := "services_inactive_page:"
	title := "❌ *Неактивные сервисы*"
	if active {
		prefix = "services_active_page:"
		title = "✅ *Активные сервисы*"
	}
	prevCb := fmt.Sprintf("%s%d", prefix, page-1)
	nextCb := fmt.Sprintf("%s%d", prefix, page+1)
	keyboard := h.createPaginationKeyboard(rows, page, totalPages, prevCb, nextCb, "services")
	header := fmt.Sprintf("%s (%d)\n\nВыберите сервис для управления:", title, total)
	h.sendEditMessageWithKeyboard(chatID, messageID, header, keyboard)
}

// showWireGuardClientsListPaged выводит список клиентов WireGuard постранично
func (h *CommandHandler) showWireGuardClientsListPaged(chatID int64, messageID int, page int) {
	clients, err := h.amneziaService.GetWireGuardClients()
	if err != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("❌ Ошибка получения списка клиентов WireGuard: %v", err))
		h.bot.Send(editMsg)
		return
	}
	total := len(clients)
	if total == 0 {
		h.sendEditMessageWithKeyboard(chatID, messageID, "📭 Нет клиентов WireGuard", h.createBackKeyboard())
		return
	}
	page, start, end, totalPages := paginate(total, page, defaultPageSize)
	var rows [][]tgbotapi.InlineKeyboardButton
	b := &strings.Builder{}
	b.WriteString("Список клиентов WireGuard:\n")
	for i := start; i < end; i++ {
		c := clients[i]
		b.WriteString(c.String())
		b.WriteString("\n")
		// Для удаления используется другое меню — здесь только обзор
	}
	prevCb := fmt.Sprintf("wg_clients_page:%d", page-1)
	nextCb := fmt.Sprintf("wg_clients_page:%d", page+1)
	keyboard := h.createPaginationKeyboard(rows, page, totalPages, prevCb, nextCb, "nav_back")
	h.sendEditMessageWithKeyboard(chatID, messageID, b.String(), keyboard)
}

// sendSimpleMessage отправляет простое сообщение с клавиатурой "Назад"

// handleCreateWireGuardClientInput обрабатывает ввод имени клиента для создания WireGuard клиента
func (h *CommandHandler) handleCreateWireGuardClientInput(ctx context.Context, update tgbotapi.Update, pendingInput PendingInput) {
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

	// Нотификация что операция началась
	executingMsg := tgbotapi.NewMessage(chatID, "⏳ Создание клиента WireGuard... Это может занять несколько секунд.")
	h.bot.Send(executingMsg)

	// Создаем задачу для worker pool
	task := workerpool.Task{
		ID: fmt.Sprintf("create_wg_client_%s", clientName),
		Handler: func() (interface{}, error) {
			_, amneziaVPNConfig, amneziaWGConfig, err := h.amneziaService.CreateWireGuardClient(clientName)
			if err != nil {
				return nil, err
			}
			return []string{amneziaVPNConfig, amneziaWGConfig}, nil
		},
		Timeout: 60 * time.Second,
	}

	// Отправляем задачу в worker pool
	h.workerPool.Submit(task)

	// Обрабатываем результат в отдельной goroutine
	go func() {
		h.handleWorkerPoolResult(task.ID, func(result workerpool.Result) {
			if result.Error != nil {
				message := fmt.Sprintf("❌ Ошибка создания клиента WireGuard: %v", result.Error)
				msg := tgbotapi.NewMessage(chatID, message)
				h.bot.Send(msg)
				return
			}

			configs, _ := result.Value.([]string)
			amneziaVPNConfig := configs[0]
			amneziaWGConfig := configs[1]

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
		})
	}()
}

// handleRemoveWireGuardClientInput обрабатывает ввод publicKey для удаления клиента WireGuard
func (h *CommandHandler) handleRemoveWireGuardClientInput(ctx context.Context, update tgbotapi.Update, pendingInput PendingInput) {
	publicKey := strings.TrimSpace(update.Message.Text)
	chatID := update.Message.Chat.ID

	// Удаляем pending input
	delete(h.pendingInputs, chatID)

	if publicKey == "" {
		message := "❌ PublicKey не может быть пустым. Попробуйте еще раз."
		msg := tgbotapi.NewMessage(chatID, message)
		h.bot.Send(msg)
		return
	}

	// Нотификация
	executingMsg := tgbotapi.NewMessage(chatID, "⏳ Удаление клиента WireGuard...")
	h.bot.Send(executingMsg)

	// Создаем задачу для worker pool
	task := workerpool.Task{
		ID: fmt.Sprintf("remove_wg_client_%s", publicKey),
		Handler: func() (interface{}, error) {
			err := h.amneziaService.RemoveWireGuardClient(publicKey)
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("✅ Клиент с PublicKey %s успешно удален.", publicKey), nil
		},
		Timeout: 30 * time.Second,
	}

	// Отправляем задачу в worker pool
	h.workerPool.Submit(task)

	// Обрабатываем результат в отдельной goroutine
	go func() {
		h.handleWorkerPoolResult(task.ID, func(result workerpool.Result) {
			if result.Error != nil {
				message := fmt.Sprintf("❌ Ошибка удаления клиента WireGuard: %v", result.Error)
				msg := tgbotapi.NewMessage(chatID, message)
				h.bot.Send(msg)
				return
			}

			message, _ := result.Value.(string)
			msg := tgbotapi.NewMessage(chatID, message)
			h.bot.Send(msg)

			// Отправляем кнопку "Назад"
			backMsg := tgbotapi.NewMessage(chatID, "Операция выполнена.")
			backMsg.ReplyMarkup = h.createBackKeyboard()
			h.bot.Send(backMsg)
		})
	}()
}

// handleRemoveWireGuardClient запрашивает у пользователя publicKey для удаления клиента WireGuard
func (h *CommandHandler) handleRemoveWireGuardClient(callback *tgbotapi.CallbackQuery) {
	// Показать список клиентов с кнопками удаления
	clients, err := h.amneziaService.GetWireGuardClients()
	if err != nil {
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, fmt.Sprintf("❌ Ошибка получения списка клиентов: %v", err))
		h.bot.Send(editMsg)
		return
	}

	if len(clients) == 0 {
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, "📭 Нет клиентов WireGuard")
		h.bot.Send(editMsg)
		return
	}

	message := "Удалить клиента WireGuard. Выберите клиента:\n"
	buttons := make([][]tgbotapi.InlineKeyboardButton, 0)
	for i, client := range clients {
		message += client.String() + "\n"
		enc := encodeKeyForCallback(client.PublicKey)
		cb := fmt.Sprintf("del_wg:%s", enc)
		label := fmt.Sprintf("Удалить %s", client.Name)
		btn := tgbotapi.NewInlineKeyboardButtonData(label, cb)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(btn))
		h.deleteMu.Lock()
		h.deleteTokenToKey[enc] = client.PublicKey
		h.deleteTokenToName[enc] = client.Name
		h.deleteTokenExpiry[enc] = time.Now()
		h.deleteMu.Unlock()
		if i >= 49 {
			break
		}
	}
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "amnezia_vpn")))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)
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
	// Клавиатура с кнопкой обновления и назад
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "wireguard_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav_back"),
		),
	)
	h.sendEditMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, message, &keyboard)
}

// handleBackupConfigs обрабатывает создание резервной копии конфигов Amnezia
func (h *CommandHandler) handleBackupConfigs(callback *tgbotapi.CallbackQuery) {
	h.handleConfigOperation(callback, "backup", "⏳ Создание резервной копии конфигурации...", "backup_configs")
}

// handleRollbackConfigs обрабатывает откат конфигурации Amnezia
func (h *CommandHandler) handleRollbackConfigs(callback *tgbotapi.CallbackQuery) {
	h.handleConfigOperation(callback, "rollback", "⏳ Выполняется откат конфигурации...", "rollback_configs")
}

// handleConfigOperation обрабатывает операции с конфигурацией Amnezia (резервное копирование и откат)
func (h *CommandHandler) handleConfigOperation(callback *tgbotapi.CallbackQuery, operationType, notificationMsg, taskID string) {
	// Нотификация пользователю, что операция выполняется
	executingMsg := tgbotapi.NewMessage(callback.Message.Chat.ID, notificationMsg)
	h.bot.Send(executingMsg)

	// Создаем задачу для worker pool
	task := workerpool.Task{
		ID: taskID,
		Handler: func() (interface{}, error) {
			// Сначала копируем папку awg с контейнера на локальный диск (временная директория)
			tmpDir, err := h.createTempDir()
			if err != nil {
				return nil, fmt.Errorf("ошибка создания временной директории: %v", err)
			}
			defer h.removeTempDir(tmpDir)

			awgDir := tmpDir + "/awg"
			if err := h.dockerService.CopyFromContainer("amnezia-awg", "/opt/amnezia/awg", tmpDir); err != nil {
				return nil, fmt.Errorf("ошибка копирования awg из контейнера: %v", err)
			}

			var resultMsg string
			switch operationType {
			case "backup":
				// Создаем резервные копии файлов
				if err := h.amneziaService.BackupConfigFiles(awgDir); err != nil {
					return nil, fmt.Errorf("ошибка создания резервных копий: %v", err)
				}
				resultMsg = "✅ Резервная копия конфигурации успешно создана!"
			case "rollback":
				// Выполняем откат конфигурации
				if err := h.amneziaService.RollbackConfigFiles(awgDir); err != nil {
					return nil, fmt.Errorf("ошибка отката конфигурации: %v", err)
				}

				// Копируем восстановленные файлы обратно в контейнер
				if err := h.dockerService.CopyToContainer("amnezia-awg", awgDir+"/clientsTable", "/opt/amnezia/awg/clientsTable"); err != nil {
					return nil, fmt.Errorf("ошибка копирования clientsTable в контейнер: %v", err)
				}
				if err := h.dockerService.CopyToContainer("amnezia-awg", awgDir+"/wg0.conf", "/opt/amnezia/awg/wg0.conf"); err != nil {
					return nil, fmt.Errorf("ошибка копирования wg0.conf в контейнер: %v", err)
				}

				// Перезапускаем WireGuard
				if err := h.amneziaService.RestartWireGuard(); err != nil {
					return nil, fmt.Errorf("ошибка перезапуска WireGuard: %v", err)
				}
				resultMsg = "✅ Конфигурация успешно восстановлена из резервной копии!"
			default:
				return nil, fmt.Errorf("неизвестная операция: %s", operationType)
			}

			return resultMsg, nil
		},
		Timeout: 60 * time.Second,
	}

	// Отправляем задачу в worker pool
	h.workerPool.Submit(task)

	// Обрабатываем результат в отдельной goroutine
	go func() {
		h.handleWorkerPoolResult(task.ID, func(result workerpool.Result) {
			if result.Error != nil {
				var message string
				switch operationType {
				case "backup":
					message = fmt.Sprintf("❌ Ошибка создания резервной копии: %v", result.Error)
				case "rollback":
					message = fmt.Sprintf("❌ Ошибка отката конфигурации: %v", result.Error)
				}
				h.sendActionResponse(callback.Message.Chat.ID, message, false)
				return
			}

			message, _ := result.Value.(string)
			h.sendActionResponse(callback.Message.Chat.ID, message, false)
		})
	}()
}

// createTempDir создает временную директорию
func (h *CommandHandler) createTempDir() (string, error) {
	tmpDir, err := os.MkdirTemp("", "amnezia-awg-")
	if err != nil {
		return "", fmt.Errorf("ошибка создания временной директории: %v", err)
	}
	return tmpDir, nil
}

// removeTempDir удаляет временную директорию
func (h *CommandHandler) removeTempDir(tmpDir string) {
	os.RemoveAll(tmpDir)
}

// handleWorkerPoolResult обрабатывает результаты из worker pool
func (h *CommandHandler) handleWorkerPoolResult(taskID string, handler func(result workerpool.Result)) {
	for result := range h.workerPool.Results() {
		if result.ID == taskID {
			handler(result)
			return
		}
	}
}

// showMainMenu отображает главное меню бота
func (h *CommandHandler) showMainMenu(chatID int64) {
	// Создание inline клавиатуры с основными командами
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус системы", "status_overview"),
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
	h.sendSimpleMessageWithKeyboard(chatID, message, &keyboard)
	// Сбрасываем стек и устанавливаем текущий экран
	h.navStack[chatID] = nil
	h.currentView[chatID] = "main"
}

// showHelp отображает справку по использованию бота
func (h *CommandHandler) showHelp(chatID int64) {
	message := `📖 *Справка по использованию бота*

Используйте меню для навигации по функциям бота.

*Доступные команды:*
/start - Показать главное меню
/help - Показать эту справку

*Основные функции:*
• 📊 Статус системы - информация о CPU, RAM, дисках
• 🐳 Контейнеры - управление Docker-контейнерами
• 🛡️ Amnezia VPN - управление WireGuard клиентами
• ⚙️ Сервисы - управление systemd-сервисами
• 🖥 Управление сервером - перезагрузка, выключение, обновления

Все действия выполняются через интерактивное меню.`

	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	h.bot.Send(msg)
}

// showStatusMenu отображает общий статус системы (упрощенное меню)
func (h *CommandHandler) showStatusMenu(chatID int64) {
	// Показываем общий статус напрямую
	fakeUpdate := h.createFakeUpdate(&tgbotapi.Chat{ID: chatID}, "")
	h.handleStatus(fakeUpdate)
}
