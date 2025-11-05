package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Manager сервис управления Docker
type Manager struct {
	socket        string
	timeoutSeconds int
}

// Container структура контейнера
type Container struct {
	ID      string
	Name    string
	Status  string
	Image   string
	Created time.Time
}

// NewManager создает новый менеджер Docker.
// timeoutSeconds задает таймаут (в секундах) для выполнения команд внутри контейнера.
// Если timeoutSeconds <= 0, будет использовано значение 10 секунд по умолчанию.
func NewManager(socket string, timeoutSeconds int) (*Manager, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}

	m := &Manager{
		socket:        socket,
		timeoutSeconds: timeoutSeconds,
	}

	// Небольшая проверка доступности Docker (не фатальная)
	cmd := exec.Command("sudo", "docker", "info")
	if out, err := cmd.CombinedOutput(); err != nil {
		// Не прерываем создание менеджера — Docker может быть недоступен на этапе тестов
		_ = out // intentionally ignored in normal flow
	}

	return m, nil
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
		if strings.TrimSpace(line) == "" {
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

	// Преобразование времени создания (fallback на zero time при ошибке)
	created, err := time.Parse("2006-01-02 15:04:05 -0700 MST", createdAt)
	if err != nil {
		created = time.Time{}
	}

	// Сокращаем ID до 12 символов если возможно
	shortID := id
	if len(id) > 12 {
		shortID = id[:12]
	}

	container := &Container{
		ID:      shortID,
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
	if !container.Created.IsZero() {
		status += fmt.Sprintf("Создан: %s\n", container.Created.Format("2006-01-02 15:04:05"))
	}

	return status, nil
}

// ReadFileFromContainer читает файл из контейнера
func (m *Manager) ReadFileFromContainer(containerName, filePath string) (string, error) {
	return m.ExecuteCommandInContainer(containerName, "cat", filePath)
}

// CopyFromContainer копирует файл или директорию из контейнера на хост.
func (m *Manager) CopyFromContainer(containerName, srcPath, dstPath string) error {
	// docker cp <container>:<src> <dst>
	args := []string{"docker", "cp", fmt.Sprintf("%s:%s", containerName, srcPath), dstPath}
	cmd := exec.Command("sudo", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка docker cp from container: %v, output: %s", err, string(output))
	}
	return nil
}

// CopyToContainer копирует файл или директорию с хоста в контейнер.
func (m *Manager) CopyToContainer(containerName, srcPath, dstPath string) error {
	// docker cp <src> <container>:<dst>
	args := []string{"docker", "cp", srcPath, fmt.Sprintf("%s:%s", containerName, dstPath)}
	cmd := exec.Command("sudo", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка docker cp to container: %v, output: %s", err, string(output))
	}
	return nil
}

// ExecuteCommandInContainer выполняет команду в контейнере
func (m *Manager) ExecuteCommandInContainer(containerName string, command ...string) (string, error) {
	// Формируем полную команду с префиксом docker exec
	args := append([]string{"docker", "exec", containerName}, command...)
	// Use context with timeout and retries
	timeout := time.Duration(m.timeoutSeconds) * time.Second
	attempts := 3
	var lastErr error
	var out []byte

	for i := 0; i < attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		// don't defer cancel inside loop; call explicitly
		cmd := exec.CommandContext(ctx, "sudo", args...)
		var err error
		out, err = cmd.CombinedOutput()
		cancel()
		if err == nil {
			return string(out), nil
		}

		// If context deadline exceeded, wrap accordingly
		if ctx.Err() == context.DeadlineExceeded {
			lastErr = fmt.Errorf("timeout after %s: %v; output: %s", timeout, ctx.Err(), string(out))
		} else {
			lastErr = fmt.Errorf("error executing command (attempt %d/%d): %v; output: %s", i+1, attempts, err, string(out))
		}

		// exponential backoff before retrying
		backoff := time.Duration(200*(i+1)) * time.Millisecond
		time.Sleep(backoff)
	}

	return string(out), fmt.Errorf("ошибка выполнения команды в контейнере %s: %v", containerName, lastErr)
}
