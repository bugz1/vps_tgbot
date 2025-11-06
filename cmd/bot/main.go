package main

import (
	"context"
	"tgbot/pkg/logger"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tgbot/internal/bot"
	"tgbot/internal/services/monitoring"
	"tgbot/internal/services/system"
	"tgbot/internal/workerpool"
	"tgbot/pkg/config"

	"github.com/spf13/viper"
)

func main() {
	// Инициализация конфигурации
	if err := initConfig(); err != nil {
		// structured log and exit
		logger.Log(logger.Error, "main.init_config_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	// Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		logger.Log(logger.Error, "main.load_config_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	// Создание worker pool
	workerPool := workerpool.New(cfg.WorkerPool.WorkersCount)
	defer workerPool.Close()

	// Создание системного монитора
	systemMonitor := system.NewMonitor()

	// Создание бота
	b, err := bot.NewBot(cfg, workerPool)
	if err != nil {
		logger.Log(logger.Error, "main.create_bot_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
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
			logger.Log(logger.Error, "main.start_bot_failed", map[string]interface{}{"error": err.Error()})
			os.Exit(1)
		}
	}()

	// Ожидание сигнала завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Создание контекста с таймаутом для graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Остановка сервиса мониторинга
	monitoringService.Stop()

	// Остановка бота с таймаутом
	done := make(chan struct{})
	go func() {
		b.Stop()
		close(done)
	}()

	select {
	case <-done:
		logger.Log(logger.Info, "bot.stopped", nil)
	case <-ctx.Done():
		logger.Log(logger.Warn, "bot.shutdown_timeout", map[string]interface{}{"error": ctx.Err().Error()})
	}
}

// initConfig инициализирует конфигурацию из файла
func initConfig() error {
	// Проверяем переменную окружения CONFIG_PATH
	configPath := os.Getenv("CONFIG_PATH")
	if configPath != "" {
		// Если задан путь через переменную окружения, используем его
		viper.SetConfigFile(configPath)
	} else {
		// Иначе ищем config.yaml в текущей директории
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
	}

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
	// Docker defaults
	viper.SetDefault("docker.bin", "docker")
	viper.SetDefault("docker.command_prefix", []string{"sudo"})
	// Default Docker command timeout (seconds)
	viper.SetDefault("docker.timeout", 10)
	viper.SetDefault("docker.tmp_dir", "/tmp")
	viper.SetDefault("docker.wg_path", "/opt/amnezia/awg")
	// cmdrunner defaults
	viper.SetDefault("cmdrunner.timeout_seconds", 10)
	viper.SetDefault("cmdrunner.attempts", 3)
	viper.SetDefault("cmdrunner.sudo_password", "")
	// workerpool defaults
	viper.SetDefault("workerpool.workers_count", 5)

	return viper.WriteConfigAs("config.yaml")
}
