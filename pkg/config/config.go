package config

import (
	"github.com/spf13/viper"
)

// Config структура конфигурации приложения
type Config struct {
	Bot        BotConfig        `mapstructure:"bot"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Docker     DockerConfig     `mapstructure:"docker"`
	CmdRunner  CmdRunnerConfig  `mapstructure:"cmdrunner"`
	WorkerPool WorkerPoolConfig `mapstructure:"workerpool"`
	Amnezia    AmneziaConfig    `mapstructure:"amnezia"`
}

// WorkerPoolConfig конфигурация worker pool
type WorkerPoolConfig struct {
	WorkersCount int `mapstructure:"workers_count"`
}

// CmdRunnerConfig параметры по умолчанию для выполнения внешних команд
type CmdRunnerConfig struct {
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
	Attempts       int    `mapstructure:"attempts"`
	SudoPassword   string `mapstructure:"sudo_password"`
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
	// Bin - это имя/путь к бинарному файлу docker (по умолчанию "docker").
	Bin string `mapstructure:"bin"`
	// CommandPrefix - это необязательный префикс для команд, например ["sudo"].
	CommandPrefix []string `mapstructure:"command_prefix"`
	// TmpDir для временных операций (копирование-редактирование-копирование)
	TmpDir string `mapstructure:"tmp_dir"`
	// WgPath - это путь внутри контейнера, где находятся файлы awg
	WgPath string `mapstructure:"wg_path"`
}

// AmneziaConfig конфигурация Amnezia VPN
type AmneziaConfig struct {
	Hostname           string            `mapstructure:"hostname"`
	Port               int               `mapstructure:"port"`
	ServerPubKey       string            `mapstructure:"server_pub_key"`
	MTU                string            `mapstructure:"mtu"`
	PersistentKeepalive string           `mapstructure:"persistent_keepalive"`
	DNS1               string            `mapstructure:"dns1"`
	DNS2               string            `mapstructure:"dns2"`
	Obfuscation        ObfuscationConfig `mapstructure:"obfuscation"`
}

// ObfuscationConfig параметры obfuscation для Amnezia VPN
type ObfuscationConfig struct {
	H1   string `mapstructure:"H1"`
	H2   string `mapstructure:"H2"`
	H3   string `mapstructure:"H3"`
	H4   string `mapstructure:"H4"`
	Jc   string `mapstructure:"Jc"`
	Jmax string `mapstructure:"Jmax"`
	Jmin string `mapstructure:"Jmin"`
	S1   string `mapstructure:"S1"`
	S2   string `mapstructure:"S2"`
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
