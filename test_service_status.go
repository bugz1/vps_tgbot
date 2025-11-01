package main

import (
	"fmt"
	"log"
	"tgbot/internal/services/system"
)

func main() {
	// Создаем монитор системы
	monitor := system.NewMonitor()

	// Тестируем получение статуса конкретного сервиса
	serviceName := "nginx" // Замените на имя существующего сервиса в вашей системе
	status, err := monitor.GetServiceStatus(serviceName)
	if err != nil {
		log.Printf("Ошибка получения статуса сервиса %s: %v", serviceName, err)
		return
	}

	fmt.Printf("Сервис: %s\n", status.Name)
	fmt.Printf("Статус: %s\n", status.Status)
	fmt.Printf("Эмодзи: %s\n", status.Emoji)

	// Тестируем получение статуса другого сервиса
	serviceName2 := "docker" // Замените на имя существующего сервиса в вашей системе
	status2, err := monitor.GetServiceStatus(serviceName2)
	if err != nil {
		log.Printf("Ошибка получения статуса сервиса %s: %v", serviceName2, err)
		return
	}

	fmt.Printf("\nСервис: %s\n", status2.Name)
	fmt.Printf("Статус: %s\n", status2.Status)
	fmt.Printf("Эмодзи: %s\n", status2.Emoji)
}
