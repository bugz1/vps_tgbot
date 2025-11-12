package formatters

import (
	"fmt"
	"sort"
	"strings"

	"tgbot/internal/services/amnezia"
)

// FormatWireGuardClients форматирует список клиентов WireGuard в строку для отображения
func FormatWireGuardClients(clients []amnezia.WireGuardClient) string {
	if len(clients) == 0 {
		return "📭 Нет клиентов WireGuard"
	}

	// Сортируем клиентов по имени для консистентного отображения
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].Name < clients[j].Name
	})

	// Формирование сообщения со списком клиентов
	message := "Список клиентов WireGuard:\n"
	for _, client := range clients {
		message += client.String() + "\n"
	}

	return message
}

// FormatWireGuardClientDetails форматирует детальную информацию о клиенте WireGuard
func FormatWireGuardClientDetails(client amnezia.WireGuardClient) string {
	var details strings.Builder

	details.WriteString(fmt.Sprintf("Имя: %s\n", client.Name))
	details.WriteString(fmt.Sprintf("PublicKey: %s\n", client.PublicKey))

	if client.DataReceived != "" {
		details.WriteString(fmt.Sprintf("Получено: %s\n", client.DataReceived))
	}

	if client.DataSent != "" {
		details.WriteString(fmt.Sprintf("Отправлено: %s\n", client.DataSent))
	}

	if client.LatestHandshake != "" {
		details.WriteString(fmt.Sprintf("Последнее подключение: %s\n", client.LatestHandshake))
	}

	status := "Неактивен"
	if client.Active {
		status = "Активен"
	}
	details.WriteString(fmt.Sprintf("Статус: %s\n", status))

	return details.String()
}
