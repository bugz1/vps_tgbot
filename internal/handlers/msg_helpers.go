package handlers

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// defaultPageSize задает размер страницы для пагинации списков
const defaultPageSize = 15

// InlineItem описывает элемент списка для построения рядов кнопок
type InlineItem struct {
	Label    string
	Callback string
}

// createBackKeyboard создает клавиатуру с кнопкой "Назад" (возврат по стеку)
func (h *CommandHandler) createBackKeyboard() *tgbotapi.InlineKeyboardMarkup {
	return h.createKeyboardWithBack(CbBack)
}

// createKeyboardWithBack создает клавиатуру с кнопкой "Назад" и указанным callback
func (h *CommandHandler) createKeyboardWithBack(backCallback string) *tgbotapi.InlineKeyboardMarkup {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", backCallback),
		),
	)
	return &keyboard
}

// createConfirmationKeyboard создает клавиатуру с кнопками подтверждения и отмены
func (h *CommandHandler) createConfirmationKeyboard(confirmText, confirmCallback, cancelText, cancelCallback string) *tgbotapi.InlineKeyboardMarkup {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(confirmText, confirmCallback),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(cancelText, cancelCallback),
		),
	)
	return &keyboard
}

// sendSimpleMessage отправляет простое сообщение с клавиатурой "Назад"
func (h *CommandHandler) sendSimpleMessage(chatID int64, message string) {
	h.sendSimpleMessageWithKeyboard(chatID, message, h.createBackKeyboard())
}

// sendSimpleMessageWithKeyboard отправляет простое сообщение с заданной клавиатурой
func (h *CommandHandler) sendSimpleMessageWithKeyboard(chatID int64, message string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	// Если сообщение слишком длинное для Telegram, отправляем как файл
	if tooLongForMessage(message) {
		h.sendDocumentBytes(chatID, "message.txt", []byte(message))
		if keyboard != nil {
			notice := tgbotapi.NewMessage(chatID, "📄 Сообщение слишком длинное — отправлено файлом.")
			notice.ReplyMarkup = keyboard
			h.bot.Send(notice)
		}
		return
	}
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.bot.Send(msg)
}

// sendEditMessageWithKeyboard отправляет редактируемое сообщение с заданной клавиатурой
func (h *CommandHandler) sendEditMessageWithKeyboard(chatID int64, messageID int, message string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	// Если текст слишком длинный для редактирования, отправляем файл и короткое уведомление
	if tooLongForMessage(message) {
		h.sendDocumentBytes(chatID, "message.txt", []byte(message))
		short := tgbotapi.NewEditMessageText(chatID, messageID, "📄 Сообщение слишком длинное — отправлено файлом.")
		short.ParseMode = "Markdown"
		short.ReplyMarkup = keyboard
		h.bot.Send(short)
		return
	}
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, message)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = keyboard
	h.bot.Send(editMsg)
}

// sendActionResponse отправляет ответ на действие пользователя
func (h *CommandHandler) sendActionResponse(chatID int64, message string, useMarkdown bool) {
	if tooLongForMessage(message) {
		h.sendDocumentBytes(chatID, "output.txt", []byte(message))
		info := tgbotapi.NewMessage(chatID, "📄 Вывод слишком длинный — отправлен файлом.")
		h.bot.Send(info)
		return
	}
	msg := tgbotapi.NewMessage(chatID, message)
	if useMarkdown {
		msg.ParseMode = "Markdown"
	}
	h.bot.Send(msg)
}

// sendDocumentBytes отправляет документ в виде байтов
func (h *CommandHandler) sendDocumentBytes(chatID int64, filename string, content []byte) {
	doc := tgbotapi.NewDocumentUpload(chatID, tgbotapi.FileBytes{Name: filename, Bytes: content})
	h.bot.Send(doc)
}

// sendError отправляет сообщение об ошибке пользователю
func (h *CommandHandler) sendError(chatID int64, operation string, err error) {
	message := fmt.Sprintf("❌ Ошибка %s: %v", operation, err)
	h.sendActionResponse(chatID, message, false)
}

// sendErrorToCallback отправляет сообщение об ошибке в ответ на callback
func (h *CommandHandler) sendErrorToCallback(callback *tgbotapi.CallbackQuery, operation string, err error) {
	message := fmt.Sprintf("❌ Ошибка %s: %v", operation, err)
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, message)
	h.bot.Send(editMsg)
}

// tooLongForMessage проверяет, слишком ли длинный текст для сообщения Telegram
func tooLongForMessage(s string) bool {
	// Запас по лимиту на Markdown/экранирование
	return len(s) > 3800
}

// escapeMarkdownBasic выполняет базовое экранирование символов Markdown (минимально безопасно)
// Используется точечно при необходимости
func escapeMarkdownBasic(s string) string {
	replacer := strings.NewReplacer("_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!")
	return replacer.Replace(s)
}

// createServiceKeyboard создает клавиатуру для управления сервисом
func (h *CommandHandler) createServiceKeyboard(serviceName string, isActive bool) *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	if isActive {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Restart", "restart_service:"+serviceName),
			tgbotapi.NewInlineKeyboardButtonData("🟥 Stop", "stop_service:"+serviceName),
		))
	} else {
		// Для остановленного сервиса показываем только Start
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🟩 Start", "start_service:"+serviceName),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📊 Status", "status_service:"+serviceName),
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "nav_back"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &keyboard
}

// createPaginationRows создает ряд(ы) пагинации с кнопками Назад/Вперед и индикатором страницы
// prevCb/nextCb должны включать нужный префикс и номер страницы
func (h *CommandHandler) createPaginationRows(page, totalPages int, prevCb, nextCb string) []tgbotapi.InlineKeyboardButton {
	label := fmt.Sprintf("Стр. %d/%d", page+1, totalPages)
	// Если нет предыдущей страницы, показываем неактивный placeholder
	var prevBtn tgbotapi.InlineKeyboardButton
	if page > 0 {
		prevBtn = tgbotapi.NewInlineKeyboardButtonData("◀️", prevCb)
	} else {
		prevBtn = tgbotapi.NewInlineKeyboardButtonData("⏹", CbNoop)
	}
	var nextBtn tgbotapi.InlineKeyboardButton
	if page+1 < totalPages {
		nextBtn = tgbotapi.NewInlineKeyboardButtonData("▶️", nextCb)
	} else {
		nextBtn = tgbotapi.NewInlineKeyboardButtonData("⏹", CbNoop)
	}
	center := tgbotapi.NewInlineKeyboardButtonData(label, CbNoop)
	return []tgbotapi.InlineKeyboardButton{prevBtn, center, nextBtn}
}

// createPaginationKeyboard создает клавиатуру с рядами элементов и нижним рядом пагинации и Назад
func (h *CommandHandler) createPaginationKeyboard(itemRows [][]tgbotapi.InlineKeyboardButton, page, totalPages int, prevCb, nextCb string, backCb string) *tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(itemRows)+2)
	rows = append(rows, itemRows...)
	rows = append(rows, h.createPaginationRows(page, totalPages, prevCb, nextCb))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(LblBack, backCb)))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &keyboard
}

// buildEntityActionKeyboard создает клавиатуру действий для сущности kind (container/service)
func (h *CommandHandler) buildEntityActionKeyboard(kind string, isActive bool, idOrName string) *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	switch kind {
	case "container":
		if isActive {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(LblRestart, CbRestartPrefix+idOrName),
				tgbotapi.NewInlineKeyboardButtonData(LblStop, CbStopPrefix+idOrName),
			))
		} else {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(LblStart, CbStartPrefix+idOrName),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(LblStatus, CbStatusPrefix+idOrName),
			tgbotapi.NewInlineKeyboardButtonData(LblLogs, CbLogsPrefix+idOrName),
		))
	case "service":
		if isActive {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(LblRestart, CbRestartServicePrefix+idOrName),
				tgbotapi.NewInlineKeyboardButtonData(LblStop, CbStopServicePrefix+idOrName),
			))
		} else {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(LblStart, CbStartServicePrefix+idOrName),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(LblStatus, CbStatusServicePrefix+idOrName),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(LblBack, CbBack)))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &kb
}

// buildPagedList универсально строит постраничный список
func (h *CommandHandler) buildPagedList(chatID int64, messageID int, title string, items []InlineItem, page int, prevCb, nextCb, backCb string) {
	page, start, end, totalPages := paginate(len(items), page, defaultPageSize)
	vis := items
	if len(items) > 0 {
		vis = items[start:end]
	}
	rows := makeItemRows(vis)
	kb := h.createPaginationKeyboard(rows, page, totalPages, prevCb, nextCb, backCb)
	h.sendEditMessageWithKeyboard(chatID, messageID, title, kb)
}

// runWithProgress отправляет “⏳ Выполняется...” и редактирует по завершении
func (h *CommandHandler) runWithProgress(chatID int64, startMsg string, keyboard *tgbotapi.InlineKeyboardMarkup, fn func() (string, bool)) {
	msg := tgbotapi.NewMessage(chatID, startMsg)
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	sent, _ := h.bot.Send(msg)
	go func(mid int) {
		out, useMarkdown := fn()
		h.sendEditMessageWithKeyboard(chatID, mid, out, keyboard)
		if useMarkdown {
			// уже включен Markdown в sendEditMessageWithKeyboard
		}
	}(sent.MessageID)
}

// paginate вычисляет безопасные границы страницы
func paginate(total, page, pageSize int) (fixedPage int, start int, end int, totalPages int) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	totalPages = (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		return 0, 0, 0, 0
	}
	fixedPage = page
	if fixedPage >= totalPages {
		fixedPage = totalPages - 1
	}
	if fixedPage < 0 {
		fixedPage = 0
	}
	start = fixedPage * pageSize
	end = start + pageSize
	if end > total {
		end = total
	}
	return
}

// makeItemRows строит ряды кнопок из элементов
func makeItemRows(items []InlineItem) [][]tgbotapi.InlineKeyboardButton {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(items))
	for _, it := range items {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(it.Label, it.Callback),
		))
	}
	return rows
}
