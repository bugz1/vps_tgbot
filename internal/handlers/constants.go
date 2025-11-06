package handlers

// Константы callback-префиксов и идентификаторов
const (
    // Навигация
    CbBack     = "nav_back"
    CbNoop     = "noop"

    // Главные разделы
    CbStatusOverview   = "status_overview"
    CbContainers       = "containers"
    CbServices         = "services"
    CbServerMgmt       = "server_management"
    CbUpdatesMgmt      = "updates_management"
    CbPowerMgmt        = "power_management"

    // Пагинация
    CbContainersPagePrefix      = "containers_page:"
    CbServicesActivePagePrefix  = "services_active_page:"
    CbServicesInactivePagePrefix= "services_inactive_page:"
    CbWgClientsPagePrefix       = "wg_clients_page:"

    // Контейнеры
    CbContainerPrefix = "container:"
    CbRestartPrefix   = "restart:"
    CbStopPrefix      = "stop:"
    CbStartPrefix     = "start:"
    CbStatusPrefix    = "status:"
    CbLogsPrefix      = "logs:"

    // Сервисы
    CbServicePrefix       = "service:"
    CbRestartServicePrefix= "restart_service:"
    CbStopServicePrefix   = "stop_service:"
    CbStartServicePrefix  = "start_service:"
    CbStatusServicePrefix = "status_service:"

    // Система
    CbReboot         = "reboot"
    CbShutdown       = "shutdown"
    CbConfirmReboot  = "confirm_reboot"
    CbConfirmShutdown= "confirm_shutdown"
    CbCheckUpdates   = "check_updates"
    CbUpgradeSystem  = "upgrade_system"

    // Amnezia / WireGuard
    CbAmneziaVPN           = "amnezia_vpn"
    CbWgListClients        = "list_wireguard_clients"
    CbWgClientMgmt         = "client_mgmt"
    CbWgMgmt               = "vpn_mgmt"
    CbWgStatus             = "wireguard_status"
    CbWgCreateClient       = "create_wireguard_client"
    CbWgRemoveClient       = "remove_wireguard_client"
    CbBackupConfigs        = "backup_configs"
    CbRollbackConfigs      = "rollback_configs"
)

// Лейблы кнопок
const (
    LblBack     = "⬅️ Назад"
    LblRestart  = "🔄 Restart"
    LblStop     = "🟥 Stop"
    LblStart    = "🟩 Start"
    LblStatus   = "📊 Status"
    LblLogs     = "📝 Logs"
    LblRefresh  = "🔄 Обновить"
)


