package main

import (
	"github.com/gin-gonic/gin"
	"go.mau.fi/whatsmeow"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var cli *whatsmeow.Client
var log waLog.Logger

// стартовый метод
func main() {

	//создаем экземпляр Engine
	engine := gin.Default()

	//маршрутизация отправки сообщения
	engine.POST("/sendMessage", sendMessage)

	//TODO:получать порт из БД

	//запускаем сервер
	err := engine.Run("127.0.0.1:10001")

	//если есть ошибка
	if err != nil {

		//выводим лог
		log.Errorf("Failed to start server: %v", err)

		//не продолжаем
		return
	}
}

// Метод отправляет сообщение
func sendMessage(ctx *gin.Context) {

	ctx.JSON(200, gin.H{
		"message": "pong",
	})
}
