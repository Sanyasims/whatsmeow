package main

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	waLog "go.mau.fi/whatsmeow/util/log"
	"os"
)

var log waLog.Logger

// стартовый метод
func main() {

	// считываем файл кофигурации
	content, err := os.ReadFile("web/config.json")

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		log.Errorf("Error when opening config file: ", err)

		//не продолжаем
		return
	}

	// объявляем структуру конфигурации
	var configuration Configuration

	// лесериализуем из JSON
	err = json.Unmarshal(content, &configuration)

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		log.Errorf("Error during parse Configuration: ", err)

		//не продолжаем
		return
	}

	//создаем экземпляр Engine
	engine := gin.Default()

	//маршрутизация отправки сообщения
	engine.POST("/sendMessage", sendMessage)

	//TODO:получать порт из БД

	//запускаем сервер
	err = engine.Run(configuration.Host)

	//если есть ошибка
	if err != nil {

		//выводим лог
		log.Errorf("Failed to start server: %v", err)

		//не продолжаем
		return
	}

	// выводим информацию
	log.Infof("Starting on %v", configuration.Host)
}

// Метод отправляет сообщение
func sendMessage(ctx *gin.Context) {

	ctx.JSON(200, gin.H{
		"message": "pong",
	})
}
