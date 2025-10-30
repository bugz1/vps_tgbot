package config

import (
	"github.com/spf13/viper"
)

// Config структура конфигурации приложения
type Config struct {
	Bot        BotConfig        `mapstructure:"bot"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Docker     DockerConfig     `mapstructure:"docker"`
}

// BotConfig конфигурация бота
type BotConfig struct {
	Token         string  `mapstructure:"token"`
	AllowedChats  []int64 `mapstructure:"allowed_chats"`
	UpdateTimeout int     `mapstructure:"update_timeout"`
}

// MonitoringConfig конфигурация мониторинга
type MonitoringConfig struct {
	CheckInterval   int `mapstructure:"check_interval"`
	CPUThreshold    int `mapstructure:"cpu_threshold"`
	MemoryThreshold int `mapstructure:"memory_threshold"`
	DiskThreshold   int `mapstructure:"disk_threshold"`
}

// DockerConfig конфигурация Docker
type DockerConfig struct {
	Socket  string `mapstructure:"socket"`
	Timeout int    `mapstructure:"timeout"`
}

// Load загружает конфигурацию из файла
func Load() (*Config, error) {
	var config Config

	// Загрузка конфигурации
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
