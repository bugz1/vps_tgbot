package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tgbot/internal/cmdrunner"
)

// Manager сервис управления Docker
type Manager struct {
	socket         string
	timeoutSeconds int
	bin string
	commandPrefix []string
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
func NewManager(socket string, timeoutSeconds int, bin string, commandPrefix []string) (*Manager, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	if bin == "" {
		bin = "docker"
	}

	m := &Manager{
		socket:         socket,
		timeoutSeconds: timeoutSeconds,
		bin:             bin,
		commandPrefix:   commandPrefix,
	}

	// Небольшая проверка доступности Docker (не фатальная), с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parts := []string{"sudo", m.bin, "info"}
	opts := cmdrunner.RunOptions{
		Timeout: 10 * time.Second,
		Attempts: 1,
		PasswordFromConfig: true,
	}
	if out, err := cmdrunner.RunWithRetries(ctx, parts, opts); err != nil {
		// Не прерываем создание менеджера — Docker может быть недоступен на этапе тестов
		_ = out // намеренно игнорируется в нормальном потоке
	}

	return m, nil
}

// ListContainers получает список контейнеров
func (m *Manager) ListContainers(containerID ...string) ([]Container, error) {
	// Формирование частей команды в зависимости от наличия ID контейнера
	parts := m.buildDockerParts(containerID...)

	out, err := m.runDockerParts(parts, time.Duration(m.timeoutSeconds)*time.Second, 3)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения списка контейнеров: %v", err)
	}

	return m.parseContainersOutput(out)
}

// buildDockerCommand формирует команду docker в зависимости от наличия ID контейнера
func (m *Manager) buildDockerParts(containerID ...string) []string {
	// формируем базовые аргументы, используя настроенный бинарный файл
	if len(containerID) > 0 && containerID[0] != "" {
		// Если передан ID контейнера, получаем информацию только о нем
		parts := []string{m.bin, "ps", "-a", "--filter", "id=" + containerID[0], "--format", "{{json .}}"}
		if len(m.commandPrefix) > 0 {
			parts = append(m.commandPrefix, parts...)
		}
		return parts
	}
	// Иначе получаем список всех контейнеров
	parts := []string{m.bin, "ps", "-a", "--format", "{{json .}}"}
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}
	return parts
}

// runDockerParts выполняет команду (части) через cmdrunner с заданным таймаутом и попытками
func (m *Manager) runDockerParts(parts []string, timeout time.Duration, attempts int) (string, error) {
	if timeout <= 0 {
		timeout = time.Duration(m.timeoutSeconds) * time.Second
	}
	if attempts <= 0 {
		attempts = 3
	}

	opts := cmdrunner.RunOptions{
		Timeout: timeout,
		Attempts: attempts,
		PasswordFromConfig: true,
	}
	out, err := cmdrunner.RunWithRetries(context.Background(), parts, opts)
	return out, err
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
	parts := []string{m.bin, "start", id}
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}
	if _, err := m.runDockerParts(parts, 30*time.Second, 3); err != nil {
		return err
	}
	return nil
}

// StopContainer останавливает контейнер
func (m *Manager) StopContainer(id string) error {
	parts := []string{m.bin, "stop", id}
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}
	if _, err := m.runDockerParts(parts, 30*time.Second, 3); err != nil {
		return err
	}
	return nil
}

// RestartContainer перезапускает контейнер
func (m *Manager) RestartContainer(id string) error {
	parts := []string{m.bin, "restart", id}
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}
	if _, err := m.runDockerParts(parts, 60*time.Second, 3); err != nil {
		return err
	}
	return nil
}

// GetContainerLogs получает логи контейнера
func (m *Manager) GetContainerLogs(id string, lines int) (string, error) {
	parts := []string{m.bin, "logs", "--tail", fmt.Sprintf("%d", lines), id}
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}
	out, err := m.runDockerParts(parts, 30*time.Second, 3)
	if err != nil {
		return "", fmt.Errorf("ошибка получения логов контейнера %s: %v", id, err)
	}
	return out, nil
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
	parts := []string{m.bin, "cp", fmt.Sprintf("%s:%s", containerName, srcPath), dstPath}
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}
	out, err := m.runDockerParts(parts, 120*time.Second, 3)
	if err != nil {
		return fmt.Errorf("ошибка docker cp from container: %v, output: %s", err, out)
	}
	return nil
}

// CopyToContainer копирует файл или директорию с хоста в контейнер.
func (m *Manager) CopyToContainer(containerName, srcPath, dstPath string) error {
	// docker cp <src> <container>:<dst>
	parts := []string{m.bin, "cp", srcPath, fmt.Sprintf("%s:%s", containerName, dstPath)}
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}
	out, err := m.runDockerParts(parts, 120*time.Second, 3)
	if err != nil {
		return fmt.Errorf("ошибка docker cp to container: %v, output: %s", err, out)
	}
	return nil
}

// ExecuteCommandInContainer выполняет команду в контейнере
func (m *Manager) ExecuteCommandInContainer(containerName string, command ...string) (string, error) {
	// Формируем полную команду с префиксом docker exec
	// Формируем полные части команды: [prefix...] [bin, exec, containerName, ...command]
	parts := []string{m.bin, "exec", containerName}
	parts = append(parts, command...)
	if len(m.commandPrefix) > 0 {
		parts = append(m.commandPrefix, parts...)
	}

	timeout := time.Duration(m.timeoutSeconds) * time.Second
	attempts := 3

	opts := cmdrunner.RunOptions{
		Timeout: timeout,
		Attempts: attempts,
		PasswordFromConfig: true,
	}
	out, err := cmdrunner.RunWithRetries(context.Background(), parts, opts)
	if err != nil {
		return out, fmt.Errorf("ошибка выполнения команды в контейнере %s: %w", containerName, err)
	}

	return out, nil
}
