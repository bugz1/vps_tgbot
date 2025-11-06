package docker_mocks

import (
	"tgbot/internal/services/docker"
)

// MockDockerManager — простая mock-реализация интерфейса DockerManager.
// В тестах можно присвоить соответствующие Func поля для настройки поведения.
type MockDockerManager struct {
	ReadFileFromContainerFunc     func(containerName, filePath string) (string, error)
	ListContainersFunc            func(containerID ...string) ([]docker.Container, error)
	StartContainerFunc            func(id string) error
	StopContainerFunc             func(id string) error
	RestartContainerFunc          func(id string) error
	GetContainerLogsFunc          func(id string, lines int) (string, error)
	GetContainerStatusFunc        func(id string) (string, error)
	ExecuteCommandInContainerFunc func(containerName string, command ...string) (string, error)
	CopyFromContainerFunc         func(containerName, srcPath, dstPath string) error
	CopyToContainerFunc           func(containerName, srcPath, dstPath string) error
}

func (m *MockDockerManager) ReadFileFromContainer(containerName, filePath string) (string, error) {
	if m != nil && m.ReadFileFromContainerFunc != nil {
		return m.ReadFileFromContainerFunc(containerName, filePath)
	}
	return "", nil
}

func (m *MockDockerManager) ListContainers(containerID ...string) ([]docker.Container, error) {
	if m != nil && m.ListContainersFunc != nil {
		return m.ListContainersFunc(containerID...)
	}
	return nil, nil
}

func (m *MockDockerManager) StartContainer(id string) error {
	if m != nil && m.StartContainerFunc != nil {
		return m.StartContainerFunc(id)
	}
	return nil
}

func (m *MockDockerManager) StopContainer(id string) error {
	if m != nil && m.StopContainerFunc != nil {
		return m.StopContainerFunc(id)
	}
	return nil
}

func (m *MockDockerManager) RestartContainer(id string) error {
	if m != nil && m.RestartContainerFunc != nil {
		return m.RestartContainerFunc(id)
	}
	return nil
}

func (m *MockDockerManager) GetContainerLogs(id string, lines int) (string, error) {
	if m != nil && m.GetContainerLogsFunc != nil {
		return m.GetContainerLogsFunc(id, lines)
	}
	return "", nil
}

func (m *MockDockerManager) GetContainerStatus(id string) (string, error) {
	if m != nil && m.GetContainerStatusFunc != nil {
		return m.GetContainerStatusFunc(id)
	}
	return "", nil
}

func (m *MockDockerManager) ExecuteCommandInContainer(containerName string, command ...string) (string, error) {
	if m != nil && m.ExecuteCommandInContainerFunc != nil {
		return m.ExecuteCommandInContainerFunc(containerName, command...)
	}
	return "", nil
}

func (m *MockDockerManager) CopyFromContainer(containerName, srcPath, dstPath string) error {
	if m != nil && m.CopyFromContainerFunc != nil {
		return m.CopyFromContainerFunc(containerName, srcPath, dstPath)
	}
	return nil
}

func (m *MockDockerManager) CopyToContainer(containerName, srcPath, dstPath string) error {
	if m != nil && m.CopyToContainerFunc != nil {
		return m.CopyToContainerFunc(containerName, srcPath, dstPath)
	}
	return nil
}
