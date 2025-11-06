package amnezia

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tgbot/internal/services/docker"
	"tgbot/pkg/logger"
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
	CopyFromContainer(containerName, srcPath, dstPath string) error
	CopyToContainer(containerName, srcPath, dstPath string) error
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
	mu            sync.Mutex
	serverConfig  *ServerConfig
}

// ServerConfig структура для хранения параметров сервера Amnezia
type ServerConfig struct {
	Hostname           string
	Port               int
	ServerPubKey       string
	MTU                string
	PersistentKeepalive string
	DNS1               string
	DNS2               string
	Obfuscation        map[string]string
}

// NewService создает новый сервис Amnezia VPN
func NewService(dockerManager DockerManager) (*Service, error) {
	service := &Service{
		dockerManager: dockerManager,
	}
	
	// Получаем реальную конфигурацию сервера из контейнера
	if err := service.updateServerConfigFromContainer(); err != nil {
		logger.Log(logger.Error, "amnezia.failed_to_load_server_config", map[string]interface{}{"error": err.Error()})
		return nil, fmt.Errorf("не удалось загрузить конфигурацию сервера: %v", err)
	}
	
	return service, nil
}

// Defaults and constants used when server config can't be read
var defaultObfuscation = map[string]string{
	"H1":   "000000000",
	"H2":   "000000000",
	"H3":   "000000000",
	"H4":   "000000000",
	"Jc":   "0",
	"Jmax": "0",
	"Jmin": "0",
	"S1":   "0",
	"S2":   "0",
}

const (
	defaultHostName            = "localhost"
	defaultPort                = 0
	defaultMTU                 = "0"
	defaultPersistentKeepAlive = "0"
	defaultDNS1                = "0.0.0.0"
	defaultDNS2                = "0.0.0.0"
	defaultServerPubKey        = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
)

// defaultServerConfig возвращает конфигурацию сервера по умолчанию
func defaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Hostname:            defaultHostName,
		Port:                defaultPort,
		ServerPubKey:        defaultServerPubKey,
		MTU:                 defaultMTU,
		PersistentKeepalive: defaultPersistentKeepAlive,
		DNS1:                defaultDNS1,
		DNS2:                defaultDNS2,
		Obfuscation:         defaultObfuscation,
	}
}

// updateServerConfigFromContainer обновляет конфигурацию сервера из файла wg0.conf в контейнере
func (s *Service) updateServerConfigFromContainer() error {
	// Читаем файл wg0.conf из контейнера amnezia-awg
	wgConfigOutput, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wg0.conf")
	if err != nil {
		return fmt.Errorf("ошибка получения конфигурации WireGuard: %v", err)
	}
	
	// Парсим конфигурацию сервера из файла
	serverConfig, err := s.parseServerConfig(wgConfigOutput)
	if err != nil {
		return fmt.Errorf("ошибка парсинга конфигурации сервера: %v", err)
	}
	
	// Обновляем конфигурацию сервера в сервисе
	s.serverConfig = serverConfig
	
	return nil
}

// parseServerConfig парсит конфигурацию сервера из содержимого файла wg0.conf
func (s *Service) parseServerConfig(config string) (*ServerConfig, error) {
	lines := strings.Split(config, "\n")
	serverConfig := defaultServerConfig()
	
	// Извлекаем параметры из секции [Interface]
	inInterfaceSection := false
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		
		// Определяем начало секции [Interface]
		if trimmedLine == "[Interface]" {
			inInterfaceSection = true
			continue
		}
		
		// Если мы вышли из секции [Interface], прекращаем обработку
		if strings.HasPrefix(trimmedLine, "[") && inInterfaceSection {
			break
		}
		
		// Обрабатываем параметры в секции [Interface]
		if inInterfaceSection {
			// Извлекаем параметры obfuscation
			if strings.HasPrefix(trimmedLine, "Jc =") || strings.HasPrefix(trimmedLine, "Jmin =") ||
				strings.HasPrefix(trimmedLine, "Jmax =") || strings.HasPrefix(trimmedLine, "S1 =") ||
				strings.HasPrefix(trimmedLine, "S2 =") || strings.HasPrefix(trimmedLine, "H1 =") ||
				strings.HasPrefix(trimmedLine, "H2 =") || strings.HasPrefix(trimmedLine, "H3 =") ||
				strings.HasPrefix(trimmedLine, "H4 =") {
				parts := strings.Split(trimmedLine, "=")
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])
					serverConfig.Obfuscation[key] = value
				}
				continue
			}
			
			// Извлекаем порт из ListenPort
			if strings.HasPrefix(trimmedLine, "ListenPort =") {
				parts := strings.Split(trimmedLine, "=")
				if len(parts) == 2 {
					portStr := strings.TrimSpace(parts[1])
					if port, err := fmt.Sscanf(portStr, "%d", &serverConfig.Port); err != nil || port != 1 {
						// Если не удалось распарсить порт, используем значение по умолчанию
						serverConfig.Port = defaultPort
					}
				}
				continue
			}
			
			// Извлекаем PublicKey сервера
			if strings.HasPrefix(trimmedLine, "PublicKey =") {
				parts := strings.Split(trimmedLine, "=")
				if len(parts) == 2 {
					serverConfig.ServerPubKey = strings.TrimSpace(parts[1])
				}
				continue
			}
		}
		
		// Извлекаем параметры из первой секции [Peer] (сервер как peer)
		if trimmedLine == "[Peer]" {
			// Продолжаем обработку для извлечения Endpoint
			continue
		}
		
		// Извлекаем Endpoint (Hostname:Port)
		if strings.HasPrefix(trimmedLine, "Endpoint =") {
			parts := strings.Split(trimmedLine, "=")
			if len(parts) == 2 {
				endpoint := strings.TrimSpace(parts[1])
				// Разделяем hostname и port
				endpointParts := strings.Split(endpoint, ":")
				if len(endpointParts) == 2 {
					serverConfig.Hostname = endpointParts[0]
					if port, err := fmt.Sscanf(endpointParts[1], "%d", &serverConfig.Port); err != nil || port != 1 {
						// Если не удалось распарсить порт, используем значение по умолчанию
						serverConfig.Port = defaultPort
					}
				}
			}
			// После извлечения Endpoint прекращаем обработку
			break
		}
	}
	
	return serverConfig, nil
}

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
		"hostName":              s.serverConfig.Hostname,
		"mtu":                   s.serverConfig.MTU,
		"persistent_keep_alive": s.serverConfig.PersistentKeepalive,
		"port":                  s.serverConfig.Port,
		"psk_key":               presharedKey,
		"server_pub_key":        s.serverConfig.ServerPubKey,
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
					"port":            fmt.Sprintf("%d", s.serverConfig.Port),
					"transport_proto": "udp",
				},
				"container": "amnezia-awg",
			},
		},
		"defaultContainer": "amnezia-awg",
		"description":      clientName,
		"dns1":             s.serverConfig.DNS1,
		"dns2":             s.serverConfig.DNS2,
		"hostName":         s.serverConfig.Hostname,
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

// writeFileToContainer записывает содержимое content в filePath внутри контейнера containerName.
// Используется base64-encoding, чтобы избежать проблем с оболочкой и экранированием при больших
// или произвольных данных (апострофы, переводы строк и т.д.).
// writeFileToContainer был заменен потоком копирования-редактирования-копирования и удален.

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
	s.mu.Lock()
	defer s.mu.Unlock()
	
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

	// Скопируем папку awg с контейнера на локальный диск (временная директория)
	tmpDir, err := os.MkdirTemp("", "amnezia-awg-")
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка создания временной директории: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := s.dockerManager.CopyFromContainer("amnezia-awg", "/opt/amnezia/awg", tmpDir); err != nil {
		return "", "", "", fmt.Errorf("ошибка копирования awg из контейнера: %v", err)
	}

	awgDir := filepath.Join(tmpDir, "awg")
	clientsTablePath := filepath.Join(awgDir, "clientsTable")
	wgPath := filepath.Join(awgDir, "wg0.conf")

	// Создаем резервные копии файлов перед изменением
	if err := s.backupConfigFiles(awgDir); err != nil {
		return "", "", "", fmt.Errorf("ошибка создания резервных копий: %v", err)
	}

	// Отложенная функция для rollback в случае ошибки
	defer func() {
		if err != nil {
			logger.Log(logger.Error, "amnezia.create_client_failed", logger.MaskSensitiveFields(map[string]interface{}{"error": err.Error()}))
			if rollbackErr := s.rollbackConfigFiles(awgDir); rollbackErr != nil {
				logger.Log(logger.Error, "amnezia.rollback_failed", logger.MaskSensitiveFields(map[string]interface{}{"error": rollbackErr.Error()}))
			}
		}
	}()

	// Получаем preshared key и clientsTable локально
	presharedKeyBytes, err := os.ReadFile(filepath.Join(awgDir, "wireguard_psk.key"))
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка чтения локального preshared ключа: %v", err)
	}
	presharedKey := strings.TrimSpace(string(presharedKeyBytes))

	clientsTableBytes, err := os.ReadFile(clientsTablePath)
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка чтения локального clientsTable: %v", err)
	}

	// Парсим clientsTable
	var clientsInfo []ClientInfo
	err = json.Unmarshal(clientsTableBytes, &clientsInfo)
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

	// Записываем обновленный clientsTable локально
	if err = os.WriteFile(clientsTablePath, updatedClientsTable, 0644); err != nil {
		return "", "", "", fmt.Errorf("ошибка записи локального clientsTable: %v", err)
	}

	// Получаем текущую конфигурацию wg0.conf
	wgConfig, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wg0.conf")
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка получения конфигурации wg0.conf: %v", err)
	}

	// Добавляем нового клиента в конфигурацию
	newPeerConfig := fmt.Sprintf("\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s\n", publicKey, presharedKey, allowedIPs)
	updatedWgConfig := wgConfig + newPeerConfig

	// Записываем обновленную конфигурацию локально
	if err = os.WriteFile(wgPath, []byte(updatedWgConfig), 0644); err != nil {
		return "", "", "", fmt.Errorf("ошибка записи локального wg0.conf: %v", err)
	}

	// Валидируем измененные файлы
	if err := s.validateConfigFiles(awgDir); err != nil {
		return "", "", "", fmt.Errorf("ошибка валидации конфигурационных файлов: %v", err)
	}

	// Копируем изменённые файлы обратно в контейнер
	if err = s.dockerManager.CopyToContainer("amnezia-awg", clientsTablePath, "/opt/amnezia/awg/clientsTable"); err != nil {
		return "", "", "", fmt.Errorf("ошибка копирования clientsTable в контейнер: %v", err)
	}
	if err = s.dockerManager.CopyToContainer("amnezia-awg", wgPath, "/opt/amnezia/awg/wg0.conf"); err != nil {
		return "", "", "", fmt.Errorf("ошибка копирования wg0.conf в контейнер: %v", err)
	}

	// Перезапускаем WireGuard сервис в контейнере
	err = s.restartWireGuard()
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка перезапуска WireGuard: %v", err)
	}

	// Создаем конфигурацию клиента для возврата
	clientConfig := s.createWireGuardConfig(privateKey, publicKey, presharedKey, allowedIPs)

	// Создаем конфигурацию в формате Amnezia VPN (закодированная)
	amneziaVPNConfig, err := s.createAmneziaVPNConfig(clientName, privateKey, publicKey, presharedKey, allowedIPs)
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка создания amnezia VPN конфигурации: %v", err)
	}

	// Создаем конфигурацию в формате AmneziaWG (текстовая)
	amneziaWGConfig, err := s.createAmneziaWGTextConfig(privateKey, publicKey, presharedKey, allowedIPs)
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка создания amnezia WG конфигурации: %v", err)
	}

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

// backupConfigFiles создает резервные копии файлов конфигурации
func (s *Service) backupConfigFiles(awgDir string) error {
	clientsPath := filepath.Join(awgDir, "clientsTable")
	wgPath := filepath.Join(awgDir, "wg0.conf")
	
	// Создаем резервную копию clientsTable
	clientsBytes, err := os.ReadFile(clientsPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения clientsTable для резервного копирования: %v", err)
	}
	
	if err := os.WriteFile(clientsPath+".backup", clientsBytes, 0644); err != nil {
		return fmt.Errorf("ошибка создания резервной копии clientsTable: %v", err)
	}
	
	// Создаем резервную копию wg0.conf
	wgBytes, err := os.ReadFile(wgPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения wg0.conf для резервного копирования: %v", err)
	}
	
	if err := os.WriteFile(wgPath+".backup", wgBytes, 0644); err != nil {
		return fmt.Errorf("ошибка создания резервной копии wg0.conf: %v", err)
	}
	
	logger.Log(logger.Info, "amnezia.backup_created", logger.MaskSensitiveFields(map[string]interface{}{
		"clientsTable": clientsPath + ".backup",
		"wg0Conf": wgPath + ".backup",
	}))
	
	return nil
}

// rollbackConfigFiles восстанавливает файлы конфигурации из резервных копий
func (s *Service) rollbackConfigFiles(awgDir string) error {
	clientsPath := filepath.Join(awgDir, "clientsTable")
	wgPath := filepath.Join(awgDir, "wg0.conf")
	
	// Восстанавливаем clientsTable из резервной копии
	clientsBackupBytes, err := os.ReadFile(clientsPath + ".backup")
	if err != nil {
		return fmt.Errorf("ошибка чтения резервной копии clientsTable: %v", err)
	}
	
	if err := os.WriteFile(clientsPath, clientsBackupBytes, 0644); err != nil {
		return fmt.Errorf("ошибка восстановления clientsTable из резервной копии: %v", err)
	}
	
	// Восстанавливаем wg0.conf из резервной копии
	wgBackupBytes, err := os.ReadFile(wgPath + ".backup")
	if err != nil {
		return fmt.Errorf("ошибка чтения резервной копии wg0.conf: %v", err)
	}
	
	if err := os.WriteFile(wgPath, wgBackupBytes, 0644); err != nil {
		return fmt.Errorf("ошибка восстановления wg0.conf из резервной копии: %v", err)
	}
	
	logger.Log(logger.Info, "amnezia.rollback_completed", logger.MaskSensitiveFields(map[string]interface{}{
		"clientsTable": clientsPath,
		"wg0Conf": wgPath,
	}))
	
	return nil
}

// validateConfigFiles проверяет валидность файлов конфигурации
func (s *Service) validateConfigFiles(awgDir string) error {
	clientsPath := filepath.Join(awgDir, "clientsTable")
	wgPath := filepath.Join(awgDir, "wg0.conf")
	
	// Проверяем валидность JSON в clientsTable
	clientsBytes, err := os.ReadFile(clientsPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения clientsTable для валидации: %v", err)
	}
	
	if !json.Valid(clientsBytes) {
		return fmt.Errorf("clientsTable содержит некорректный JSON")
	}
	
	// Проверяем структуру clientsTable
	var clientsInfo []ClientInfo
	if err := json.Unmarshal(clientsBytes, &clientsInfo); err != nil {
		return fmt.Errorf("ошибка парсинга clientsTable: %v", err)
	}
	
	// Проверяем синтаксис wg0.conf (простая проверка на наличие секций)
	wgBytes, err := os.ReadFile(wgPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения wg0.conf для валидации: %v", err)
	}
	
	wgText := string(wgBytes)
	if !strings.Contains(wgText, "[Interface]") {
		return fmt.Errorf("wg0.conf не содержит секцию [Interface]")
	}
	
	logger.Log(logger.Info, "amnezia.config_validation_passed", logger.MaskSensitiveFields(map[string]interface{}{
		"clientsTable": clientsPath,
		"wg0Conf": wgPath,
	}))
	
	return nil
}

// BackupConfigFiles создает резервные копии файлов конфигурации
func (s *Service) BackupConfigFiles(awgDir string) error {
	return s.backupConfigFiles(awgDir)
}

// RollbackConfigFiles восстанавливает файлы конфигурации из резервных копий
func (s *Service) RollbackConfigFiles(awgDir string) error {
	return s.rollbackConfigFiles(awgDir)
}

// ValidateConfigFiles проверяет валидность файлов конфигурации
func (s *Service) ValidateConfigFiles(awgDir string) error {
	return s.validateConfigFiles(awgDir)
}

// RestartWireGuard перезапускает WireGuard сервис в контейнере
func (s *Service) RestartWireGuard() error {
	return s.restartWireGuard()
}

// GetDockerManager возвращает менеджер Docker
func (s *Service) GetDockerManager() DockerManager {
	return s.dockerManager
}

// UpdateServerConfig обновляет конфигурацию сервера из файла wg0.conf в контейнере
func (s *Service) UpdateServerConfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	return s.updateServerConfigFromContainer()
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
func (s *Service) createAmneziaVPNConfig(clientName, privateKey, publicKey, presharedKey, allowedIPs string) (string, error) {
	// Извлекаем IP адрес из allowedIPs (например, "10.8.1.2/32" -> "10.8.1.2")
	_ = strings.Split(allowedIPs, "/")[0]

	// Получаем параметры obfuscation из серверной конфигурации
	wgConfig, err := s.dockerManager.ReadFileFromContainer("amnezia-awg", "/opt/amnezia/awg/wg0.conf")
	if err != nil {
		return "", fmt.Errorf("ошибка чтения wg0.conf из контейнера: %v", err)
	}
	obf := s.extractObfuscationParamsFromConfig(wgConfig)

	jsonData, err := s.buildAmneziaConfigJSON(obf, clientName, privateKey, publicKey, presharedKey, allowedIPs)
	if err != nil {
		return "", err
	}
	if len(jsonData) == 0 {
		return "", fmt.Errorf("пустой json при создании amnezia config")
	}

	return encodeVPNPayload(jsonData), nil
}

// createAmneziaWGTextConfig создает конфигурацию в формате AmneziaWG (текстовая)
func (s *Service) createAmneziaWGTextConfig(privateKey, publicKey, presharedKey, allowedIPs string) (string, error) {
	// Берем параметры или дефолты
	get := func(k string) string {
		if v := s.serverConfig.Obfuscation[k]; v != "" {
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
PersistentKeepalive = %s`, allowedIPs, s.serverConfig.DNS1, s.serverConfig.DNS2, privateKey, jc, jmin, jmax, s1, s2, h1, h2, h3, h4, publicKey, presharedKey, s.serverConfig.Hostname, s.serverConfig.Port, s.serverConfig.PersistentKeepalive), nil
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
Endpoint = %s:%d
PersistentKeepalive = %s`, privateKey, allowedIPs, publicKey, presharedKey, s.serverConfig.Hostname, s.serverConfig.Port, s.serverConfig.PersistentKeepalive)
}

// RemoveWireGuardClient удаляет клиента WireGuard по его PublicKey.
// Реализовано через копирование папки awg из контейнера, правку локальных файлов и копирование обратно.
func (s *Service) RemoveWireGuardClient(publicKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Создаем временную директорию
	tmpDir, err := os.MkdirTemp("", "amnezia-awg-")
	if err != nil {
		return fmt.Errorf("ошибка создания временной директории: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Копируем папку awg из контейнера
	if err := s.dockerManager.CopyFromContainer("amnezia-awg", "/opt/amnezia/awg", tmpDir); err != nil {
		return fmt.Errorf("ошибка копирования awg из контейнера: %v", err)
	}

	awgDir := filepath.Join(tmpDir, "awg")
	clientsPath := filepath.Join(awgDir, "clientsTable")
	wgPath := filepath.Join(awgDir, "wg0.conf")

	// Создаем резервные копии файлов перед изменением
	if err := s.backupConfigFiles(awgDir); err != nil {
		return fmt.Errorf("ошибка создания резервных копий: %v", err)
	}

	// Отложенная функция для rollback в случае ошибки
	defer func() {
		if err != nil {
			logger.Log(logger.Error, "amnezia.remove_client_failed", logger.MaskSensitiveFields(map[string]interface{}{"error": err.Error()}))
			if rollbackErr := s.rollbackConfigFiles(awgDir); rollbackErr != nil {
				logger.Log(logger.Error, "amnezia.rollback_failed", logger.MaskSensitiveFields(map[string]interface{}{"error": rollbackErr.Error()}))
			}
		}
	}()

	// Читаем clientsTable
	clientsBytes, err := os.ReadFile(clientsPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения локального clientsTable: %v", err)
	}

	// Проверяем валидность JSON
	if !json.Valid(clientsBytes) {
		return fmt.Errorf("clientsTable содержит некорректный JSON")
	}

	var clientsInfo []ClientInfo
	if err := json.Unmarshal(clientsBytes, &clientsInfo); err != nil {
		return fmt.Errorf("ошибка парсинга clientsTable: %v", err)
	}

	// Находим клиента по PublicKey
	idx := -1
	for i, c := range clientsInfo {
		if c.ClientID == publicKey {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("клиент с PublicKey %s не найден в clientsTable", publicKey)
	}

	// Удаляем запись
	clientsInfo = append(clientsInfo[:idx], clientsInfo[idx+1:]...)

	// Сериализуем обратно
	updatedClients, err := json.MarshalIndent(clientsInfo, "", "    ")
	if err != nil {
		return fmt.Errorf("ошибка сериализации clientsTable: %v", err)
	}

	// Записываем локально
	if err := os.WriteFile(clientsPath, updatedClients, 0644); err != nil {
		return fmt.Errorf("ошибка записи локального clientsTable: %v", err)
	}

	// Читаем wg0.conf и удаляем блок [Peer] с нужным PublicKey
	wgBytes, err := os.ReadFile(wgPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения локального wg0.conf: %v", err)
	}

	wgText := string(wgBytes)

	// Диагностический лог: сколько блоков [Peer] найдено до операции
	beforeBlocksCount := strings.Count(wgText, "[Peer]")
	logger.Log(logger.Info, "amnezia.remove_start", logger.MaskSensitiveFields(map[string]interface{}{"publicKey": logger.MaskKey(publicKey), "peer_blocks_before": beforeBlocksCount}))

	// Парсим блоки построчно: собираем блоки между [Peer] и следующей секцией
	lines := strings.Split(wgText, "\n")
	var outLines []string
	inPeer := false
	var currentBlock []string
	skipBlock := false

	flushBlock := func() {
		if !skipBlock && len(currentBlock) > 0 {
			outLines = append(outLines, currentBlock...)
		}
		currentBlock = nil
		skipBlock = false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[Peer]") {
			// flush previous peer if any
			if inPeer {
				flushBlock()
			}
			inPeer = true
			currentBlock = []string{line}
			skipBlock = false
			continue
		}

		if inPeer {
			currentBlock = append(currentBlock, line)
			if strings.HasPrefix(trimmed, "PublicKey") {
				parts := strings.SplitN(trimmed, "=", 2)
				if len(parts) == 2 && strings.TrimSpace(parts[1]) == publicKey {
					skipBlock = true
				}
			}
			continue
		}

		// not in peer
		outLines = append(outLines, line)
	}
	if inPeer {
		flushBlock()
	}

	newWg := strings.Join(outLines, "\n")

	// Проверка: убедимся, что точная строка 'PublicKey = <publicKey>' больше не встречается в результирующем тексте
	if strings.Contains(newWg, "PublicKey = "+publicKey) || strings.Contains(newWg, "PublicKey="+publicKey) {
		logger.Log(logger.Error, "amnezia.remove_failed_still_present", logger.MaskSensitiveFields(map[string]interface{}{"publicKey": logger.MaskKey(publicKey)}))
		return fmt.Errorf("не удалось удалить PublicKey %s из wg0.conf", publicKey)
	}

	if err := os.WriteFile(wgPath, []byte(newWg), 0644); err != nil {
		return fmt.Errorf("ошибка записи локального wg0.conf: %v", err)
	}

	// Валидируем измененные файлы
	if err := s.validateConfigFiles(awgDir); err != nil {
		return fmt.Errorf("ошибка валидации конфигурационных файлов: %v", err)
	}

	afterBlocksCount := strings.Count(newWg, "[Peer]")
	removedCount := beforeBlocksCount - afterBlocksCount
	logger.Log(logger.Info, "amnezia.remove_done", logger.MaskSensitiveFields(map[string]interface{}{"publicKey": logger.MaskKey(publicKey), "removed_blocks": removedCount}))

	// Копируем файлы обратно в контейнер
	if err := s.dockerManager.CopyToContainer("amnezia-awg", clientsPath, "/opt/amnezia/awg/clientsTable"); err != nil {
		return fmt.Errorf("ошибка копирования clientsTable в контейнер: %v", err)
	}
	if err := s.dockerManager.CopyToContainer("amnezia-awg", wgPath, "/opt/amnezia/awg/wg0.conf"); err != nil {
		return fmt.Errorf("ошибка копирования wg0.conf в контейнер: %v", err)
	}

	// Перезапускаем WireGuard
	if err := s.restartWireGuard(); err != nil {
		return fmt.Errorf("ошибка перезапуска WireGuard: %v", err)
	}

	return nil
}
