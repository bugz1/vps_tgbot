package system

import (
	"fmt"
	"math"
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// CPUInfo структура для информации о CPU
type CPUInfo struct {
	Model     string
	Cores     int
	Frequency float64
	Load      float64
}

// MemoryInfo структура для информации о памяти
type MemoryInfo struct {
	Total       float64
	Used        float64
	Free        float64
	UsedPercent float64
	SwapTotal   float64
	SwapUsed    float64
	SwapPercent float64
}

// DiskInfo структура для информации о диске
type DiskInfo struct {
	MountPoint  string
	FileSystem  string
	Total       float64
	Used        float64
	Free        float64
	UsedPercent float64
}

// Monitor сервис мониторинга системы
type Monitor struct{}

// NewMonitor создает новый монитор системы
func NewMonitor() *Monitor {
	return &Monitor{}
}

// GetCPUInfo получает информацию о CPU
func (m *Monitor) GetCPUInfo() (*CPUInfo, error) {
	// Получение информации о CPU
	cpuInfo, err := cpu.Info()
	if err != nil {
		return nil, err
	}

	// Получение загрузки CPU
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		return nil, err
	}

	if len(cpuInfo) == 0 {
		return nil, fmt.Errorf("информация о CPU недоступна")
	}

	info := cpuInfo[0]
	load := cpuPercent[0]

	return &CPUInfo{
		Model:     info.ModelName,
		Cores:     int(info.Cores),
		Frequency: info.Mhz,
		Load:      load,
	}, nil
}

// GetCPUInfoString получает информацию о CPU в виде строки (для совместимости)
func (m *Monitor) GetCPUInfoString() (string, error) {
	cpuInfo, err := m.GetCPUInfo()
	if err != nil {
		return "Информация о CPU недоступна", err
	}

	return fmt.Sprintf(
		"Модель: %s\nЯдер: %d\nЧастота: %.2f MHz\nЗагрузка: %.2f%%",
		cpuInfo.Model, cpuInfo.Cores, cpuInfo.Frequency, cpuInfo.Load,
	), nil
}

// GetMemoryInfo получает информацию о памяти
func (m *Monitor) GetMemoryInfo() (*MemoryInfo, error) {
	// Получение информации о памяти
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	// Получение информации о swap
	swapInfo, err := mem.SwapMemory()
	if err != nil {
		return nil, err
	}

	return &MemoryInfo{
		Total:       bytesToGB(memInfo.Total),
		Used:        bytesToGB(memInfo.Used),
		Free:        bytesToGB(memInfo.Free),
		UsedPercent: memInfo.UsedPercent,
		SwapTotal:   bytesToGB(swapInfo.Total),
		SwapUsed:    bytesToGB(swapInfo.Used),
		SwapPercent: swapInfo.UsedPercent,
	}, nil
}

// GetMemoryInfoString получает информацию о памяти в виде строки (для совместимости)
func (m *Monitor) GetMemoryInfoString() (string, error) {
	memInfo, err := m.GetMemoryInfo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"Всего: %.2f GB\nИспользовано: %.2f GB\nСвободно: %.2f GB\nЗагрузка: %.2f%%\n\nSwap: %.2f GB / %.2f GB (%.2f%%)",
		memInfo.Total, memInfo.Used, memInfo.Free, memInfo.UsedPercent,
		memInfo.SwapUsed, memInfo.SwapTotal, memInfo.SwapPercent,
	), nil
}

// GetDiskInfo получает информацию о дисках
func (m *Monitor) GetDiskInfo() ([]*DiskInfo, error) {
	// Получение информации о дисках
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil, err
	}

	var diskInfos []*DiskInfo
	for _, partition := range partitions {
		// Пропускаем временные файловые системы
		if partition.Fstype == "tmpfs" || partition.Fstype == "devtmpfs" {
			continue
		}

		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			continue
		}

		diskInfos = append(diskInfos, &DiskInfo{
			MountPoint:  partition.Mountpoint,
			FileSystem:  partition.Fstype,
			Total:       bytesToGB(usage.Total),
			Used:        bytesToGB(usage.Used),
			Free:        bytesToGB(usage.Free),
			UsedPercent: usage.UsedPercent,
		})
	}

	if len(diskInfos) == 0 {
		return nil, fmt.Errorf("информация о дисках недоступна")
	}

	return diskInfos, nil
}

// GetDiskInfoString получает информацию о дисках в виде строки (для совместимости)
func (m *Monitor) GetDiskInfoString() (string, error) {
	diskInfos, err := m.GetDiskInfo()
	if err != nil {
		return "Информация о дисках недоступна", err
	}

	result := ""
	for _, diskInfo := range diskInfos {
		result += fmt.Sprintf(
			"%s (%s):\n  Всего: %.2f GB\n  Использовано: %.2f GB\n  Свободно: %.2f GB\n  Загрузка: %.2f%%\n\n",
			diskInfo.MountPoint, diskInfo.FileSystem,
			diskInfo.Total, diskInfo.Used, diskInfo.Free, diskInfo.UsedPercent,
		)
	}

	return result, nil
}

// bytesToGB конвертирует байты в гигабайты
func bytesToGB(bytes uint64) float64 {
	return math.Round(float64(bytes)/1024/1024/1024*100) / 100
}

// Reboot перезагружает сервер
func (m *Monitor) Reboot() error {
	cmd := exec.Command("sudo", "reboot")
	return cmd.Run()
}

// Shutdown выключает сервер
func (m *Monitor) Shutdown() error {
	cmd := exec.Command("sudo", "shutdown", "-h", "now")
	return cmd.Run()
}

// CheckUpdates проверяет доступные обновления системы
func (m *Monitor) CheckUpdates() (string, error) {
	// Выполняем команду для обновления списка пакетов
	updateCmd := exec.Command("sudo", "apt", "update")
	_, err := updateCmd.Output()
	if err != nil {
		return "", fmt.Errorf("ошибка обновления списка пакетов: %v", err)
	}

	// Выполняем команду для проверки доступных обновлений
	cmd := exec.Command("sudo", "apt", "list", "--upgradable")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ошибка проверки обновлений: %v", err)
	}

	return string(output), nil
}

// UpgradeSystem обновляет систему
func (m *Monitor) UpgradeSystem() error {
	// Выполняем команду для обновления системы
	cmd := exec.Command("sudo", "apt", "upgrade", "-y")
	return cmd.Run()
}

// GetServices получает список запущенных systemd сервисов через D-Bus
func (m *Monitor) GetServices() ([]string, error) {
	// Подключаемся к системной шине D-Bus
	conn, err := dbus.SystemBus()
	if err != nil {
		// Если не удалось подключиться к D-Bus, используем fallback метод
		return m.getServicesFallback()
	}
	defer conn.Close()

	// Получаем объект менеджера systemd
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")

	// Вызываем метод ListUnits для получения списка всех юнитов
	var units []struct {
		Name        string
		Description string
		LoadState   string
		ActiveState string
		SubState    string
		Followed    string
		ObjectPath  dbus.ObjectPath
		JobId       uint32
		JobType     string
		JobPath     dbus.ObjectPath
	}
	err = obj.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units)
	if err != nil {
		// Если не удалось получить список юнитов через D-Bus, используем fallback метод
		return m.getServicesFallback()
	}

	// Фильтруем только запущенные сервисы
	var services []string
	for _, unit := range units {
		// Проверяем, что это сервис и он активен
		if strings.HasSuffix(unit.Name, ".service") && unit.ActiveState == "active" {
			// Удаляем суффикс .service из имени
			serviceName := strings.TrimSuffix(unit.Name, ".service")
			services = append(services, serviceName)
		}
	}

	return services, nil
}

// getServicesFallback получает список сервисов через вызов systemctl (fallback метод)
func (m *Monitor) getServicesFallback() ([]string, error) {
	cmd := exec.Command("sudo", "systemctl", "list-units", "--type=service", "--state=running", "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения команды systemctl: %v", err)
	}

	services := m.parseServicesOutput(string(output))
	return services, nil
}

// parseServicesOutput парсит вывод команды systemctl
func (m *Monitor) parseServicesOutput(output string) []string {
	lines := strings.Split(output, "\n")
	var services []string

	// Пропускаем заголовки и пустые строки
	for _, line := range lines {
		if strings.Contains(line, ".service") && !strings.HasPrefix(line, "UNIT") && strings.TrimSpace(line) != "" {
			// Извлекаем имя сервиса из строки
			fields := strings.Fields(line)
			if len(fields) > 0 {
				serviceName := strings.TrimSuffix(fields[0], ".service")
				services = append(services, serviceName)
			}
		}
	}

	return services
}
