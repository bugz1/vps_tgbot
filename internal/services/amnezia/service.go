package amnezia

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tgbot/internal/services/docker"
)

// DockerManager интерфейс для работы с Docker
type DockerManager interface {
	ReadFileFromContainer(containerName, filePath string) (string, error)
	ListContainers(containerID ...string) ([]docker.Container, error)
	StartContainer(id string) error
	StopContainer(id string) error
	RestartContainer(id string) error
	GetContainerLogs(id string, lines int) (string, error)
	GetContainerStatus(id string) (string, error)
	ExecuteCommandInContainer(containerName string, command ...string) (string, error)
}

// WireGuardClient информация о клиенте WireGuard
// ClientInfo структура для парсинга JSON данных из clientsTable
type ClientInfo struct {
	ClientID string   `json:"clientId"`
	UserData UserData `json:"userData"`
}

// UserData структура для парсинга пользовательских данных клиента
type UserData struct {
	ClientName      string `json:"clientName"`
	CreationDate    string `json:"creationDate"`
	AllowedIps      string `json:"allowedIps,omitempty"`
	DataReceived    string `json:"dataReceived,omitempty"`
	DataSent        string `json:"dataSent,omitempty"`
	LatestHandshake string `json:"latestHandshake,omitempty"`
}

type WireGuardClient struct {
	Name            string
	PublicKey       string
	DataReceived    string
	DataSent        string
	LatestHandshake string
	Active          bool
}

// String возвращает строковое представление клиента WireGuard в формате с эмодзи
func (wgc WireGuardClient) String() string {
	if wgc.LatestHandshake != "" {
		// Зеленый квадратик для онлайн клиентов
		return fmt.Sprintf("🟩 %s %s/%s", wgc.Name, wgc.DataReceived, wgc.DataSent)
	} else {
		// Красный квадратик для оффлайн клиентов
		return fmt.Sprintf("🟥 %s", wgc.Name)
	}
}

// Service сервис для работы с Amnezia VPN
type Service struct {
	dockerManager DockerManager
}

// NewService создает новый сервис Amnezia VPN
func NewService(dockerManager DockerManager) *Service {
	return &Service{
		dockerManager: dockerManager,
	}
}

// Defaults and constants used when server config can't be read
var defaultObfuscation = map[string]string{
	"H1":   "990542262",
	"H2":   "585238767",
	"H3":   "1758137553",
	"H4":   "1885521486",
	"Jc":   "2",
	"Jmax": "50",
	"Jmin": "10",
	"S1":   "66",
	"S2":   "99",
}

const (
	defaultHostName            = "bugz1.online"
	defaultPort                = 31662
	defaultMTU                 = "1376"
	defaultPersistentKeepAlive = "25"
	defaultDNS1                = "8.8.8.8"
	defaultDNS2                = "8.8.4.4"
	defaultServerPubKey        = "+OQy7FAMYVOSRxYyk3CkyLzCX7bmogbdq1qubpMf1i4="
)

// encodeVPNPayload compresses jsonData with zlib, prefixes a 4-byte big-endian length header,
// encodes with URL-safe base64 without padding and returns a vpn:// URL string.
func encodeVPNPayload(jsonData []byte) string {
	var compressed bytes.Buffer
	writer, err := zlib.NewWriterLevel(&compressed, 8)
	if err != nil {
		// Shouldn't happen; return empty to preserve previous behavior
		return ""
	}
	_, err = writer.Write(jsonData)
	if err != nil {
		writer.Close()
		return ""
	}
	writer.Close()

	uncompressedLength := len(jsonData)
	header := []byte{
		byte(uncompressedLength >> 24),
		byte(uncompressedLength >> 16),
		byte(uncompressedLength >> 8),
		byte(uncompressedLength),
	}

	dataWithHeader := append(header, compressed.Bytes()...)
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(dataWithHeader)
	return "vpn://" + encoded
}

// buildAmneziaConfigJSON constructs the JSON payload used inside the vpn:// encoding.
// obf contains obfuscation params (H1,H2,H3,H4,Jc,Jmax,Jmin,S1,S2). Missing keys are filled with defaults.
func (s *Service) buildAmneziaConfigJSON(obf map[string]string, clientName, privateKey, publicKey, presharedKey, allowedIPs string) ([]byte, error) {
	// fill missing obfuscation params from defaults
	params := make(map[string]string)
	for k, v := range defaultObfuscation {
		params[k] = v
	}
	for k, v := range obf {
		if v != "" {
			params[k] = v
		}
	}

	lastConfig := map[string]interface{}{
		"H1":   params["H1"],
		"H2":   params["H2"],
		"H3":   params["H3"],
		"H4":   params["H4"],
		"Jc":   params["Jc"],
		"Jmax": params["Jmax"],
		"Jmin": params["Jmin"],
		"S1":   params["S1"],
		"S2":   params["S2"],
		"allowed_ips": []string{
			"0.0.0.0/0",
			"::/0",
		},
		"clientId":              publicKey,
		"client_ip":             strings.Split(allowedIPs, "/")[0],
		"client_priv_key":       privateKey,
		"client_pub_key":        publicKey,
		"config":                s.createWireGuardConfig(privateKey, publicKey, presharedKey, allowedIPs),
		"hostName":              defaultHostName,
		"mtu":                   defaultMTU,
		"persistent_keep_alive": defaultPersistentKeepAlive,
		"port":                  defaultPort,
		"psk_key":               presharedKey,
		"server_pub_key":        defaultServerPubKey,
	}

	// top-level config
	config := map[string]interface{}{
		"containers": []map[string]interface{}{
			{
				"awg": map[string]interface{}{
					"H1":              params["H1"],
					"H2":              params["H2"],
					"H3":              params["H3"],
					"H4":              params["H4"],
					"Jc":              params["Jc"],
					"Jmax":            params["Jmax"],
					"Jmin":            params["Jmin"],
					"S1":              params["S1"],
					"S2":              params["S2"],
					"last_config":     string(mustMarshalJSON(lastConfig)),
					"port":            fmt.Sprintf("%d", defaultPort),
					"transport_proto": "udp",
				},
				"container": "amnezia-awg",
			},
		},
		"defaultContainer": "amnezia-awg",
		"description":      clientName,
		"dns1":             defaultDNS1,
		"dns2":             defaultDNS2,
		"hostName":         defaultHostName,
	}

	return json.Marshal(config)
}

// mustMarshalJSON marshals value to JSON; on error returns empty JSON string (used only for nesting last_config)
func mustMarshalJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// GetWireGuardClients получает список клиентов WireGuard из контейнера amnezia-awg
func (s *Service) GetWireGuardClients() ([]WireGuardClient, error) {
	// Читаем файл clientsTable из контейнера amnezia-awg
	clientsTableOutput, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/clientsTable")
	if err != nil {
		return nil, fmt.Errorf("ошибка получения списка клиентов WireGuard: %v", err)
	}

	// Читаем файл wg0.conf из контейнера amnezia-awg
	wgConfigOutput, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wg0.conf")
	if err != nil {
		return nil, fmt.Errorf("ошибка получения конфигурации WireGuard: %v", err)
	}

	// Выполняем команду wg show для получения реального статуса клиентов
	wgShowOutput, err := s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "wg", "show")
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения команды wg show: %v", err)
	}

	// Парсим вывод команды wg show для получения актуальной информации о клиентах
	wgClients, err := s.parseWgShowOutput(wgShowOutput)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга вывода wg show: %v", err)
	}

	// Парсим JSON из файла clientsTable
	var clientsInfo []ClientInfo
	err = json.Unmarshal([]byte(clientsTableOutput), &clientsInfo)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга clientsTable: %v", err)
	}

	// Создаем карту клиентов из clientsTable для быстрого поиска по PublicKey
	clientInfoMap := make(map[string]ClientInfo)
	for _, clientInfo := range clientsInfo {
		clientInfoMap[clientInfo.ClientID] = clientInfo
	}

	// Создаем финальный список клиентов с объединенной информацией
	var clients []WireGuardClient
	for _, wgClient := range wgClients {
		// Ищем информацию о клиенте в clientsTable по PublicKey
		if clientInfo, exists := clientInfoMap[wgClient.PublicKey]; exists {
			// Объединяем информацию из clientsTable и wg show
			client := WireGuardClient{
				Name:            clientInfo.UserData.ClientName,
				PublicKey:       wgClient.PublicKey,
				DataReceived:    wgClient.DataReceived,
				DataSent:        wgClient.DataSent,
				LatestHandshake: wgClient.LatestHandshake,
				Active:          wgClient.Active,
			}
			clients = append(clients, client)
		} else {
			// Если клиент не найден в clientsTable, используем только информацию из wg show
			clients = append(clients, wgClient)
		}
	}

	// Парсим файл wg0.conf для извлечения PublicKey клиентов, которые могут отсутствовать в wg show
	wgConfigLines := strings.Split(wgConfigOutput, "\n")
	var currentPeer struct {
		allowedIPs string
		publicKey  string
	}

	// Карта для отслеживания уже добавленных клиентов по PublicKey
	addedClients := make(map[string]bool)
	for _, client := range clients {
		addedClients[client.PublicKey] = true
	}

	for _, line := range wgConfigLines {
		line = strings.TrimSpace(line)

		// Поиск секции клиента
		if strings.HasPrefix(line, "[Peer]") {
			// Если у нас есть предыдущий пир с заполненными данными, добавляем его если он еще не добавлен
			if currentPeer.allowedIPs != "" && currentPeer.publicKey != "" {
				if !addedClients[currentPeer.publicKey] {
					// Ищем информацию о клиенте в clientsTable по PublicKey
					var client WireGuardClient
					if clientInfo, exists := clientInfoMap[currentPeer.publicKey]; exists {
						client = WireGuardClient{
							Name:      clientInfo.UserData.ClientName,
							PublicKey: currentPeer.publicKey,
							Active:    false, // Клиент не активен, так как его нет в wg show
						}
					} else {
						// Если клиент не найден в clientsTable, создаем минимальную запись
						client = WireGuardClient{
							Name:      "Unknown",
							PublicKey: currentPeer.publicKey,
							Active:    false,
						}
					}
					clients = append(clients, client)
					addedClients[currentPeer.publicKey] = true
				}
			}
			// Сбрасываем данные для нового пира
			currentPeer.allowedIPs = ""
			currentPeer.publicKey = ""
			continue
		}

		// Извлекаем AllowedIPs
		if strings.HasPrefix(line, "AllowedIPs") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				currentPeer.allowedIPs = strings.TrimSpace(parts[1])
			}
			continue
		}

		// Извлекаем PublicKey
		if strings.HasPrefix(line, "PublicKey") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				currentPeer.publicKey = strings.TrimSpace(parts[1])
			}
			continue
		}
	}

	// Не забываем сохранить данные последнего пира
	if currentPeer.allowedIPs != "" && currentPeer.publicKey != "" {
		if !addedClients[currentPeer.publicKey] {
			// Ищем информацию о клиенте в clientsTable по PublicKey
			var client WireGuardClient
			if clientInfo, exists := clientInfoMap[currentPeer.publicKey]; exists {
				client = WireGuardClient{
					Name:      clientInfo.UserData.ClientName,
					PublicKey: currentPeer.publicKey,
					Active:    false, // Клиент не активен, так как его нет в wg show
				}
			} else {
				// Если клиент не найден в clientsTable, создаем минимальную запись
				client = WireGuardClient{
					Name:      "Unknown",
					PublicKey: currentPeer.publicKey,
					Active:    false,
				}
			}
			clients = append(clients, client)
			addedClients[currentPeer.publicKey] = true
		}
	}

	return clients, nil
}

// parseWgShowOutput парсит вывод команды wg show и возвращает список клиентов WireGuard
func (s *Service) parseWgShowOutput(output string) ([]WireGuardClient, error) {
	lines := strings.Split(output, "\n")
	var clients []WireGuardClient
	var currentClient *WireGuardClient

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Пропускаем пустые строки и заголовки интерфейса
		if line == "" || strings.HasPrefix(line, "interface:") {
			continue
		}

		// Новый пир
		if strings.HasPrefix(line, "peer:") {
			// Сохраняем предыдущего клиента, если он существует
			if currentClient != nil {
				clients = append(clients, *currentClient)
			}

			// Создаем нового клиента
			parts := strings.Split(line, " ")
			if len(parts) >= 2 {
				currentClient = &WireGuardClient{
					PublicKey: parts[1],
					Active:    false, // По умолчанию клиент не активен
				}
			}
			continue
		}

		// Если у нас есть текущий клиент, обрабатываем его свойства
		if currentClient != nil {
			if strings.HasPrefix(line, "endpoint:") {
				// Endpoint не нужен для нашего списка клиентов
				continue
			} else if strings.HasPrefix(line, "allowed ips:") {
				// Allowed IPs не нужен для нашего списка клиентов
				continue
			} else if strings.HasPrefix(line, "latest handshake:") {
				// Извлекаем время последнего хендшейка
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					handshakeInfo := strings.TrimSpace(strings.Join(parts[1:], ":"))
					// Форматируем время последнего хендшейка с единицами измерения
					currentClient.LatestHandshake = s.formatHandshakeTime(handshakeInfo)

					// Определяем, активен ли клиент (если последний хендшейк был менее 3 минут назад)
					currentClient.Active = s.isClientActive(handshakeInfo)
				}
			} else if strings.HasPrefix(line, "transfer:") {
				// Извлекаем информацию о передаче данных
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					transferInfo := strings.TrimSpace(strings.Join(parts[1:], ":"))
					// Разделяем полученные и отправленные данные
					transferParts := strings.Split(transferInfo, ",")
					if len(transferParts) >= 2 {
						// Извлекаем полученные данные (первую часть)
						receivedPart := strings.TrimSpace(transferParts[0])
						if strings.Contains(receivedPart, "received") {
							// Добавляем единицы измерения
							dataReceived := strings.TrimSpace(strings.Split(receivedPart, " ")[0])
							if strings.Contains(receivedPart, "KiB") {
								currentClient.DataReceived = dataReceived + " KiB"
							} else if strings.Contains(receivedPart, "MiB") {
								currentClient.DataReceived = dataReceived + " MiB"
							} else if strings.Contains(receivedPart, "GiB") {
								currentClient.DataReceived = dataReceived + " GiB"
							} else if strings.Contains(receivedPart, "TiB") {
								currentClient.DataReceived = dataReceived + " TiB"
							} else {
								currentClient.DataReceived = dataReceived + " B"
							}
						}

						// Извлекаем отправленные данные (вторую часть)
						sentPart := strings.TrimSpace(transferParts[1])
						if strings.Contains(sentPart, "sent") {
							// Добавляем единицы измерения
							dataSent := strings.TrimSpace(strings.Split(sentPart, " ")[0])
							if strings.Contains(sentPart, "KiB") {
								currentClient.DataSent = dataSent + " KiB"
							} else if strings.Contains(sentPart, "MiB") {
								currentClient.DataSent = dataSent + " MiB"
							} else if strings.Contains(sentPart, "GiB") {
								currentClient.DataSent = dataSent + " GiB"
							} else if strings.Contains(sentPart, "TiB") {
								currentClient.DataSent = dataSent + " TiB"
							} else {
								currentClient.DataSent = dataSent + " B"
							}
						}
					}
				}
			}
		}
	}

	// Не забываем добавить последнего клиента
	if currentClient != nil {
		clients = append(clients, *currentClient)
	}

	return clients, nil
}

// isClientActive определяет, активен ли клиент на основе времени последнего хендшейка
func (s *Service) isClientActive(handshakeInfo string) bool {
	if handshakeInfo == "" {
		return false
	}

	// Обрезаем слово "ago" если присутствует
	handshakeInfo = strings.TrimSpace(strings.TrimSuffix(handshakeInfo, "ago"))

	// Быстрые проверки
	if strings.Contains(handshakeInfo, "second") || strings.Contains(handshakeInfo, "seconds") {
		return true
	}

	if strings.Contains(handshakeInfo, "minute") || strings.Contains(handshakeInfo, "minutes") {
		// Попытаться извлечь число минут перед словом minute(s)
		parts := strings.Fields(handshakeInfo)
		for i, p := range parts {
			if p == "minute" || p == "minutes" {
				if i > 0 {
					var n int
					if _, err := fmt.Sscanf(parts[i-1], "%d", &n); err == nil {
						return n < 3
					}
				}
				break
			}
		}
		// Если не распарсили — предположим активным (поведение старой функции)
		return true
	}

	// Если указаны часы/дни/never — считаем неактивным
	if strings.Contains(handshakeInfo, "hour") || strings.Contains(handshakeInfo, "hours") ||
		strings.Contains(handshakeInfo, "day") || strings.Contains(handshakeInfo, "days") ||
		strings.Contains(handshakeInfo, "never") {
		return false
	}

	// Для любых других форматов — считать неактивным (чаще безопаснее)
	return false
}

// formatHandshakeTime форматирует время последнего хендшейка с добавлением единиц измерения
func (s *Service) formatHandshakeTime(handshakeInfo string) string {
	if handshakeInfo == "" {
		return "никогда"
	}

	// Убираем слово "ago" в конце
	handshakeInfo = strings.TrimSpace(strings.TrimSuffix(handshakeInfo, "ago"))

	// Добавляем единицы измерения на русском языке
	if strings.Contains(handshakeInfo, "seconds") {
		handshakeInfo = strings.Replace(handshakeInfo, "seconds", "секунд", -1)
		handshakeInfo = strings.Replace(handshakeInfo, "second", "секунда", -1)
	} else if strings.Contains(handshakeInfo, "minutes") {
		handshakeInfo = strings.Replace(handshakeInfo, "minutes", "минут", -1)
		handshakeInfo = strings.Replace(handshakeInfo, "minute", "минута", -1)
	} else if strings.Contains(handshakeInfo, "hours") {
		handshakeInfo = strings.Replace(handshakeInfo, "hours", "часов", -1)
		handshakeInfo = strings.Replace(handshakeInfo, "hour", "час", -1)
	} else if strings.Contains(handshakeInfo, "days") {
		handshakeInfo = strings.Replace(handshakeInfo, "days", "дней", -1)
		handshakeInfo = strings.Replace(handshakeInfo, "day", "день", -1)
	} else if strings.Contains(handshakeInfo, "weeks") {
		handshakeInfo = strings.Replace(handshakeInfo, "weeks", "недель", -1)
		handshakeInfo = strings.Replace(handshakeInfo, "week", "неделя", -1)
	} else if strings.Contains(handshakeInfo, "months") {
		handshakeInfo = strings.Replace(handshakeInfo, "months", "месяцев", -1)
		handshakeInfo = strings.Replace(handshakeInfo, "month", "месяц", -1)
	} else if strings.Contains(handshakeInfo, "years") {
		handshakeInfo = strings.Replace(handshakeInfo, "years", "лет", -1)
		handshakeInfo = strings.Replace(handshakeInfo, "year", "год", -1)
	}

	return handshakeInfo
}

// CreateWireGuardClient создает нового клиента WireGuard в контейнере amnezia-awg
func (s *Service) CreateWireGuardClient(clientName string) (string, string, string, error) {
	// Генерируем ключи для нового клиента
	privateKeyOutput, err := s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "wg", "genkey")
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка генерации приватного ключа: %v", err)
	}
	privateKey := strings.TrimSpace(privateKeyOutput)

	// Генерируем публичный ключ из приватного
	publicKeyOutput, err := s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "bash", "-c", fmt.Sprintf("echo '%s' | wg pubkey", privateKey))
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка генерации публичного ключа: %v", err)
	}
	publicKey := strings.TrimSpace(publicKeyOutput)

	// Получаем preshared key из существующего файла
	presharedKey, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wireguard_psk.key")
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка чтения preshared ключа: %v", err)
	}
	presharedKey = strings.TrimSpace(presharedKey)

	// Получаем текущий clientsTable
	clientsTableOutput, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/clientsTable")
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка получения clientsTable: %v", err)
	}

	// Парсим clientsTable
	var clientsInfo []ClientInfo
	err = json.Unmarshal([]byte(clientsTableOutput), &clientsInfo)
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка парсинга clientsTable: %v", err)
	}

	// Определяем следующий IP адрес для клиента
	nextIP := len(clientsInfo) + 1
	allowedIPs := fmt.Sprintf("10.8.1.%d/32", nextIP)

	// Создаем новую запись клиента
	newClient := ClientInfo{
		ClientID: publicKey,
		UserData: UserData{
			ClientName:   clientName,
			CreationDate: time.Now().Format("Mon Jan 2 15:04:05 2006"),
			AllowedIps:   allowedIPs,
		},
	}
	clientsInfo = append(clientsInfo, newClient)

	// Преобразуем обновленный clientsTable в JSON
	updatedClientsTable, err := json.MarshalIndent(clientsInfo, "", "    ")
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка сериализации clientsTable: %v", err)
	}

	// Записываем обновленный clientsTable в контейнер через echo
	_, err = s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "bash", "-c", fmt.Sprintf("echo '%s' > /opt/amnezia/awg/clientsTable", string(updatedClientsTable)))
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка записи clientsTable в контейнер: %v", err)
	}

	// Получаем текущую конфигурацию wg0.conf
	wgConfig, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wg0.conf")
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка получения конфигурации wg0.conf: %v", err)
	}

	// Добавляем нового клиента в конфигурацию
	newPeerConfig := fmt.Sprintf("\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s\n", publicKey, presharedKey, allowedIPs)
	updatedWgConfig := wgConfig + newPeerConfig

	// Записываем обновленную конфигурацию в контейнер через echo
	_, err = s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "bash", "-c", fmt.Sprintf("echo '%s' > /opt/amnezia/awg/wg0.conf", updatedWgConfig))
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка записи конфигурации wg0.conf в контейнер: %v", err)
	}

	// Перезапускаем WireGuard сервис в контейнере
	err = s.restartWireGuard()
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка перезапуска WireGuard: %v", err)
	}

	// Создаем конфигурацию клиента для возврата
	clientConfig := s.createWireGuardConfig(privateKey, publicKey, presharedKey, allowedIPs)

	// Создаем конфигурацию в формате Amnezia VPN (закодированная)
	amneziaVPNConfig := s.createAmneziaVPNConfig(clientName, privateKey, publicKey, presharedKey, allowedIPs)

	// Создаем конфигурацию в формате AmneziaWG (текстовая)
	amneziaWGConfig := s.createAmneziaWGTextConfig(privateKey, publicKey, presharedKey, allowedIPs)

	return clientConfig, amneziaVPNConfig, amneziaWGConfig, nil
}

// restartWireGuard перезапускает WireGuard сервис в контейнере
func (s *Service) restartWireGuard() error {
	// Останавливаем WireGuard сервис
	_, err := s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "wg-quick", "down", "/opt/amnezia/awg/wg0.conf")
	if err != nil {
		// Игнорируем ошибку, если интерфейс не был запущен
	}

	// Запускаем WireGuard сервис
	_, err = s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "wg-quick", "up", "/opt/amnezia/awg/wg0.conf")
	if err != nil {
		return fmt.Errorf("ошибка запуска WireGuard: %v", err)
	}

	return nil
}

// extractObfuscationParamsFromConfig извлекает параметры obfuscation из конфигурации
func (s *Service) extractObfuscationParamsFromConfig(config string) map[string]string {
	params := make(map[string]string)
	lines := strings.Split(config, "\n")

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		// Проверяем строки с параметрами obfuscation в секции [Interface]
		if strings.HasPrefix(trimmedLine, "Jc =") || strings.HasPrefix(trimmedLine, "Jmin =") ||
			strings.HasPrefix(trimmedLine, "Jmax =") || strings.HasPrefix(trimmedLine, "S1 =") ||
			strings.HasPrefix(trimmedLine, "S2 =") || strings.HasPrefix(trimmedLine, "H1 =") ||
			strings.HasPrefix(trimmedLine, "H2 =") || strings.HasPrefix(trimmedLine, "H3 =") ||
			strings.HasPrefix(trimmedLine, "H4 =") {
			parts := strings.Split(trimmedLine, "=")
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				params[key] = value
			}
		}
	}

	return params
}

// createAmneziaVPNConfig создает конфигурацию в формате Amnezia VPN (закодированная)
func (s *Service) createAmneziaVPNConfig(clientName, privateKey, publicKey, presharedKey, allowedIPs string) string {
	// Извлекаем IP адрес из allowedIPs (например, "10.8.1.2/32" -> "10.8.1.2")
	_ = strings.Split(allowedIPs, "/")[0]

	// Получаем параметры obfuscation из серверной конфигурации
	wgConfig, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wg0.conf")
	var obf map[string]string
	if err != nil {
		// Use defaults by leaving obf nil/empty
		obf = map[string]string{}
	} else {
		obf = s.extractObfuscationParamsFromConfig(wgConfig)
	}

	jsonData, err := s.buildAmneziaConfigJSON(obf, clientName, privateKey, publicKey, presharedKey, allowedIPs)
	if err != nil || len(jsonData) == 0 {
		return ""
	}

	return encodeVPNPayload(jsonData)
}

// createAmneziaWGTextConfig создает конфигурацию в формате AmneziaWG (текстовая)
func (s *Service) createAmneziaWGTextConfig(privateKey, publicKey, presharedKey, allowedIPs string) string {
	// Получаем параметры obfuscation из серверной конфигурации
	wgConfig, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wg0.conf")
	var obf map[string]string
	if err != nil {
		obf = map[string]string{}
	} else {
		obf = s.extractObfuscationParamsFromConfig(wgConfig)
	}

	// Берем параметры или дефолты
	get := func(k string) string {
		if v := obf[k]; v != "" {
			return v
		}
		return defaultObfuscation[k]
	}

	jc := get("Jc")
	jmin := get("Jmin")
	jmax := get("Jmax")
	s1 := get("S1")
	s2 := get("S2")
	h1 := get("H1")
	h2 := get("H2")
	h3 := get("H3")
	h4 := get("H4")

	return fmt.Sprintf(`[Interface]
Address = %s
DNS = %s, %s
PrivateKey = %s
Jc = %s
Jmin = %s
Jmax = %s
S1 = %s
S2 = %s
H1 = %s
H2 = %s
H3 = %s
H4 = %s

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = %s:%d
PersistentKeepalive = %s`, allowedIPs, defaultDNS1, defaultDNS2, privateKey, jc, jmin, jmax, s1, s2, h1, h2, h3, h4, publicKey, presharedKey, defaultHostName, defaultPort, defaultPersistentKeepAlive)
}

// GetWireGuardStatus получает статус WireGuard интерфейса
func (s *Service) GetWireGuardStatus() (string, error) {
	// Выполняем команду wg show для получения статуса WireGuard
	output, err := s.dockerManager.ExecuteCommandInContainer("amnezia-awg", "wg", "show")
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения команды wg show: %v", err)
	}

	return output, nil
}

// createWireGuardConfig создает конфигурацию WireGuard в текстовом формате
func (s *Service) createWireGuardConfig(privateKey, publicKey, presharedKey, allowedIPs string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = 1.1.1.1

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0
Endpoint = bugz1.online:31662
PersistentKeepalive = 25`, privateKey, allowedIPs, publicKey, presharedKey)
}
