package monitoring

import (
	"fmt"
	"time"

	"tgbot/internal/services/system"
	"tgbot/pkg/config"
	"tgbot/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Service сервис мониторинга системных событий
type Service struct {
	bot           *tgbotapi.BotAPI
	config        *config.Config
	systemService *system.Monitor
	chatID        int64
	stopChan      chan struct{}
}

// NewService создает новый сервис мониторинга
func NewService(bot *tgbotapi.BotAPI, cfg *config.Config, systemService *system.Monitor, chatID int64) *Service {
	return &Service{
		bot:           bot,
		config:        cfg,
		systemService: systemService,
		chatID:        chatID,
		stopChan:      make(chan struct{}),
	}
}

// Start запускает сервис мониторинга
func (s *Service) Start() {
	go s.monitor()
}

// Stop останавливает сервис мониторинга
func (s *Service) Stop() {
	close(s.stopChan)
}

// monitor выполняет периодическую проверку системных метрик
func (s *Service) monitor() {
	ticker := time.NewTicker(time.Duration(s.config.Monitoring.CheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkSystemMetrics()
		case <-s.stopChan:
			return
		}
	}
}

// checkSystemMetrics проверяет системные метрики и отправляет уведомления при достижении пороговых значений
func (s *Service) checkSystemMetrics() {
	// Проверка загрузки CPU
	if s.config.Monitoring.CPUThreshold > 0 {
		cpuInfo, err := s.systemService.GetCPUInfo()
		if err != nil {
			logger.Log(logger.Error, "monitoring.get_cpu_failed", map[string]interface{}{"error": err.Error()})
			return
		}

		if cpuInfo.Load > float64(s.config.Monitoring.CPUThreshold) {
			message := fmt.Sprintf("⚠️ Высокая нагрузка на CPU: %.2f%% (порог: %d%%)", cpuInfo.Load, s.config.Monitoring.CPUThreshold)
			logger.Log(logger.Warn, "monitoring.cpu_threshold_exceeded", map[string]interface{}{"load": cpuInfo.Load, "threshold": s.config.Monitoring.CPUThreshold})
			s.sendNotification(message)
		}
	}

	// Проверка использования памяти
	if s.config.Monitoring.MemoryThreshold > 0 {
		memInfo, err := s.systemService.GetMemoryInfo()
		if err != nil {
			logger.Log(logger.Error, "monitoring.get_memory_failed", map[string]interface{}{"error": err.Error()})
			return
		}

		if memInfo.UsedPercent > float64(s.config.Monitoring.MemoryThreshold) {
			message := fmt.Sprintf("⚠️ Высокое использование памяти: %.2f%% (порог: %d%%)", memInfo.UsedPercent, s.config.Monitoring.MemoryThreshold)
			logger.Log(logger.Warn, "monitoring.memory_threshold_exceeded", map[string]interface{}{"used_percent": memInfo.UsedPercent, "threshold": s.config.Monitoring.MemoryThreshold})
			s.sendNotification(message)
		}
	}

	// Проверка использования диска
	if s.config.Monitoring.DiskThreshold > 0 {
		diskInfos, err := s.systemService.GetDiskInfo()
		if err != nil {
			logger.Log(logger.Error, "monitoring.get_disks_failed", map[string]interface{}{"error": err.Error()})
			return
		}

		for _, diskInfo := range diskInfos {
			// Проверяем, что свободное место меньше порога (100 - usedPercent > threshold)
			freePercent := 100 - diskInfo.UsedPercent
			if freePercent < float64(s.config.Monitoring.DiskThreshold) {
				message := fmt.Sprintf("⚠️ Мало свободного места на диске %s: %.2f%% свободно (порог: %d%%)",
					diskInfo.MountPoint, freePercent, s.config.Monitoring.DiskThreshold)
				logger.Log(logger.Warn, "monitoring.disk_threshold_exceeded", map[string]interface{}{"mount": diskInfo.MountPoint, "free_percent": freePercent, "threshold": s.config.Monitoring.DiskThreshold})
				s.sendNotification(message)
			}
		}
	}
}

// sendNotification отправляет уведомление в Telegram
func (s *Service) sendNotification(message string) {
	msg := tgbotapi.NewMessage(s.chatID, message)
	_, err := s.bot.Send(msg)
	if err != nil {
		// Попытка отправить уведомление об ошибке администратору
		errorMsg := fmt.Sprintf("❌ Ошибка отправки уведомления: %v\nСообщение: %s", err, message)
		logger.Log(logger.Error, "monitoring.send_notification_failed", map[string]interface{}{"error": err.Error(), "message": message})

		// Если у нас есть список разрешенных чатов, попробуем отправить в первый из них
		if len(s.config.Bot.AllowedChats) > 0 {
			msg := tgbotapi.NewMessage(s.config.Bot.AllowedChats[0], errorMsg)
			s.bot.Send(msg)
		}
	}
}
