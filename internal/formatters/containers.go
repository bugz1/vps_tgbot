package formatters

import (
	"fmt"
	"sort"
	"strings"
	
	"tgbot/internal/services/docker"
)

// FormatContainers форматирует список контейнеров в строку для отображения
func FormatContainers(containers []docker.Container) string {
	if len(containers) == 0 {
		return "📭 Нет запущенных контейнеров"
	}

	// Сортируем контейнеры по имени для консистентного отображения
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Name < containers[j].Name
	})

	var builder strings.Builder
	builder.WriteString("Список контейнеров:\n\n")
	
	for _, container := range containers {
		// Используем эмодзи для визуального обозначения статуса
		var statusEmoji string
		switch {
		case strings.HasPrefix(container.Status, "Up"):
			statusEmoji = "🟢"
		case strings.Contains(container.Status, "Exited"):
			statusEmoji = "🔴"
		default:
			statusEmoji = "🟡"
		}
		
		builder.WriteString(fmt.Sprintf("%s %s [%s]\n", statusEmoji, container.Name, container.Status))
	}

	return builder.String()
}

// FormatContainerDetails форматирует детальную информацию о контейнере
func FormatContainerDetails(container docker.Container) string {
	var details strings.Builder
	
	details.WriteString(fmt.Sprintf("ID: %s\n", container.ID))
	details.WriteString(fmt.Sprintf("Имя: %s\n", container.Name))
	details.WriteString(fmt.Sprintf("Статус: %s\n", container.Status))
	
	if container.Image != "" {
		details.WriteString(fmt.Sprintf("Образ: %s\n", container.Image))
	}
	
	if !container.Created.IsZero() {
		details.WriteString(fmt.Sprintf("Создан: %s\n", container.Created.Format("2006-01-02 15:04:05")))
	}
	
	return details.String()
}