package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"tgbot/internal/bot"
	"tgbot/internal/services/monitoring"
	"tgbot/internal/services/system"
	"tgbot/pkg/config"

	"github.com/spf13/viper"
)

func main() {
	// Инициализация конфигурации
	if err := initConfig(); err != nil {
		log.Fatalf("Ошибка инициализации конфигурации: %v", err)
	}

	// Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	// Создание системного монитора
	systemMonitor := system.NewMonitor()

	// Создание бота
	b, err := bot.NewBot(cfg)
	if err != nil {
		log.Fatalf("Ошибка создания бота: %v", err)
	}

	// Создание и запуск сервиса мониторинга
	// Используем первый разрешенный чат для отправки уведомлений
	var monitoringChatID int64
	if len(cfg.Bot.AllowedChats) > 0 {
		monitoringChatID = cfg.Bot.AllowedChats[0]
	}

	monitoringService := monitoring.NewService(b.GetAPI(), cfg, systemMonitor, monitoringChatID)
	monitoringService.Start()

	// Запуск бота
	go func() {
		if err := b.Start(); err != nil {
			log.Fatalf("Ошибка запуска бота: %v", err)
		}
	}()

	// Ожидание сигнала завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Остановка сервиса мониторинга
	monitoringService.Stop()

	// Остановка бота
	b.Stop()
	log.Println("Бот остановлен")
}

// initConfig инициализирует конфигурацию из файла
func initConfig() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	// Чтение конфигурации
	if err := viper.ReadInConfig(); err != nil {
		// Если файл конфигурации не найден, создаем его с значениями по умолчанию
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return createDefaultConfig()
		}
		return err
	}

	return nil
}

// createDefaultConfig создает файл конфигурации с значениями по умолчанию
func createDefaultConfig() error {
	viper.SetDefault("bot.token", "YOUR_TELEGRAM_BOT_TOKEN")
	viper.SetDefault("bot.allowed_chats", []int64{123456789})
	viper.SetDefault("bot.update_timeout", 60)
	viper.SetDefault("monitoring.check_interval", 30)
	viper.SetDefault("monitoring.cpu_threshold", 90)
	viper.SetDefault("monitoring.disk_threshold", 10)
	viper.SetDefault("docker.socket", "/var/run/docker.sock")
	viper.SetDefault("docker.timeout", 30)

	return viper.WriteConfigAs("config.yaml")
}
