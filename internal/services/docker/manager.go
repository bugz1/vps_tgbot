package docker

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Manager сервис управления Docker
type Manager struct {
	socket string
}

// Container структура контейнера
type Container struct {
	ID      string
	Name    string
	Status  string
	Image   string
	Created time.Time
}

// NewManager создает новый менеджер Docker
func NewManager(socket string) (*Manager, error) {
	// Проверка доступности Docker
	cmd := exec.Command("sudo", "docker", "info")
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("docker недоступен: %v", err)
	}

	return &Manager{
		socket: socket,
	}, nil
}

// ListContainers получает список контейнеров
func (m *Manager) ListContainers(containerID ...string) ([]Container, error) {
	// Формирование команды в зависимости от наличия ID контейнера
	cmd := m.buildDockerCommand(containerID...)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения списка контейнеров: %v", err)
	}

	return m.parseContainersOutput(string(output))
}

// buildDockerCommand формирует команду docker в зависимости от наличия ID контейнера
func (m *Manager) buildDockerCommand(containerID ...string) *exec.Cmd {
	if len(containerID) > 0 && containerID[0] != "" {
		// Если передан ID контейнера, получаем информацию только о нем
		return exec.Command("sudo", "docker", "ps", "-a", "--filter", "id="+containerID[0], "--format", "{{json .}}")
	}
	// Иначе получаем список всех контейнеров
	return exec.Command("sudo", "docker", "ps", "-a", "--format", "{{json .}}")
}

// parseContainersOutput парсит вывод команды docker ps
func (m *Manager) parseContainersOutput(output string) ([]Container, error) {
	containers := make([]Container, 0)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		container, err := m.parseContainerLine(line)
		if err != nil {
			// Пропускаем строки с ошибками парсинга
			continue
		}

		containers = append(containers, *container)
	}

	return containers, nil
}

// parseContainerLine парсит одну строку вывода docker ps
func (m *Manager) parseContainerLine(line string) (*Container, error) {
	// Парсинг JSON
	var containerInfo map[string]interface{}
	err := json.Unmarshal([]byte(line), &containerInfo)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON: %v", err)
	}

	// Проверка наличия необходимых полей
	id, idOk := containerInfo["ID"].(string)
	names, namesOk := containerInfo["Names"].(string)
	status, statusOk := containerInfo["Status"].(string)
	image, imageOk := containerInfo["Image"].(string)
	createdAt, createdAtOk := containerInfo["CreatedAt"].(string)

	if !idOk || !namesOk || !statusOk || !imageOk || !createdAtOk {
		return nil, fmt.Errorf("отсутствуют необходимые поля в данных контейнера")
	}

	// Преобразование времени создания
	created, err := time.Parse("2006-01-02 15:04:05 -0700 MST", createdAt)
	if err != nil {
		// Используем zero time если не удалось распарсить
		created = time.Time{}
	}

	container := &Container{
		ID:      id[:12], // Сокращаем ID до 12 символов
		Name:    names,
		Status:  status,
		Image:   image,
		Created: created,
	}

	return container, nil
}

// StartContainer запускает контейнер
func (m *Manager) StartContainer(id string) error {
	cmd := exec.Command("sudo", "docker", "start", id)
	return cmd.Run()
}

// StopContainer останавливает контейнер
func (m *Manager) StopContainer(id string) error {
	cmd := exec.Command("sudo", "docker", "stop", id)
	return cmd.Run()
}

// RestartContainer перезапускает контейнер
func (m *Manager) RestartContainer(id string) error {
	cmd := exec.Command("sudo", "docker", "restart", id)
	return cmd.Run()
}

// GetContainerLogs получает логи контейнера
func (m *Manager) GetContainerLogs(id string, lines int) (string, error) {
	cmd := exec.Command("sudo", "docker", "logs", "--tail", fmt.Sprintf("%d", lines), id)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ошибка получения логов контейнера %s: %v", id, err)
	}

	return string(output), nil
}

// GetContainerStatus получает статус контейнера
func (m *Manager) GetContainerStatus(id string) (string, error) {
	// Получение информации о контейнере через ListContainers
	containers, err := m.ListContainers(id)
	if err != nil {
		return "", fmt.Errorf("ошибка получения статуса контейнера %s: %v", id, err)
	}

	if len(containers) == 0 {
		return "", fmt.Errorf("контейнер %s не найден", id)
	}

	// Получение контейнера из списка
	container := containers[0]

	// Формирование статуса из информации контейнера
	status := fmt.Sprintf("ID: %s\n", container.ID)
	status += fmt.Sprintf("Имя: %s\n", container.Name)
	status += fmt.Sprintf("Статус: %s\n", container.Status)
	status += fmt.Sprintf("Образ: %s\n", container.Image)
	status += fmt.Sprintf("Создан: %s\n", container.Created.Format("2006-01-02 15:04:05"))

	return status, nil
}
