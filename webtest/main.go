package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/gin-gonic/gin"
	"go.mau.fi/whatsmeow/webtest/properties"
	"go.mau.fi/whatsmeow/webtest/webhook"
	"go.mau.fi/whatsmeow/webtest/ws"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/protobuf/proto"

	waBinary "go.mau.fi/whatsmeow/binary"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Стартовый метод
func main() {

	// создаем instance
	properties.InstanceWa = properties.Instance{
		Log:             waLog.Stdout("Main", "DEBUG", true),
		DbLog:           waLog.Stdout("Database", "DEBUG", true),
		DebugLogs:       flag.Bool("debug", true, "Enable debug logs?"),
		DbDialect:       flag.String("db-dialect", "sqlite3", "Database dialect (sqlite3 or postgres)"),
		DbAddress:       flag.String("db-address", "file:webtest.db?_foreign_keys=on", "Database address"),
		RequestFullSync: flag.Bool("request-full-sync", false, "Request full (1 year) history sync when logging in?"),
		PairRejectChan:  make(chan bool, 1),
		HistorySyncID:   0,
		StartupTime:     time.Now().Unix(),
	}

	waBinary.IndentXML = true

	flag.Parse()

	if *properties.InstanceWa.RequestFullSync {

		store.DeviceProps.RequireFullSync = proto.Bool(true)
	}

	properties.InstanceWa.Log.Infof("Run app")

	// считываем файл кофигурации
	content, err := os.ReadFile("config.json")

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		properties.InstanceWa.Log.Errorf("Error when opening config file: %v", err)

		// не продолжаем
		return
	}

	// лесериализуем из JSON
	err = json.Unmarshal(content, &properties.InstanceWa.Config)

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		properties.InstanceWa.Log.Errorf("Error during parse Configuration: %v", err)

		//не продолжаем
		return
	}

	//создаем экземпляр Engine
	engine := gin.Default()

	//websocket
	engine.GET("/ws", wsHandle)

	//маршрутизация запуска инстанса
	engine.GET("/runInstance", runInstance)

	//маршрутизация отправки сообщения
	engine.POST("/sendMessage", sendMessage)

	//запускаем сервер
	err = engine.Run(properties.InstanceWa.Config.Host)

	//если есть ошибка
	if err != nil {

		//выводим лог
		properties.InstanceWa.Log.Errorf("Failed to start server: %v", err)

		//не продолжаем
		return
	}
}

// Метод запускает инстанс
func runInstance(ctx *gin.Context) {

	// запускаем инстанс в отдельном потоке
	go properties.StartInstance()

	// отдаем ответ
	ctx.JSON(200, gin.H{
		"success": true,
	})
}

// Метод отправляет сообщение
func sendMessage(ctx *gin.Context) {

	// считываем тело запроса
	content, err := io.ReadAll(ctx.Request.Body)

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		properties.InstanceWa.Log.Errorf("Error read body request: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		//не продолжаем
		return
	}

	// объявляем структуру отправки текстового сообщения
	var requestSendMessage properties.RequestSendMessage

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestSendMessage)

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		properties.InstanceWa.Log.Errorf("Error during parse RequestSendMessage: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		//не продолжаем
		return
	}

	//TODO проверять валидность данных

	// парсим идентифкатор Whatsapp, если chatId то его
	recipient, ok := properties.ParseJID(strconv.FormatInt(requestSendMessage.Phone, 10))

	// если не ок
	if !ok {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		//отдаем
		return
	}

	// кодируем сообщение
	msg := &waProto.Message{Conversation: proto.String(requestSendMessage.Message)}

	// отправляем сообщение
	resp, err := properties.InstanceWa.Client.SendMessage(context.Background(), recipient, msg)

	// если есть ошибка
	if err != nil {

		// выводим ошибку
		properties.InstanceWa.Log.Errorf("Error sending message: %v", err)

		// отдаем ответ
		ctx.JSON(500, gin.H{
			"reason": "Error sending message",
		})

	} else {

		//выводим лог
		properties.InstanceWa.Log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)

		// отдаем ответ
		ctx.JSON(200, gin.H{
			"id": resp.ID,
		})

		//создаем структуру вебхук о статусе сообщения
		statusMessageWebhook := webhook.StatusMessageWebhook{
			TypeWebhook:     "statusMessage",
			WebhookUrl:      properties.InstanceWa.Config.WebhookUrl,
			CountTrySending: 0,
			InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
				IdInstance: 0,
				Wid:        properties.InstanceWa.Client.Store.ID.User + "@c.us",
			},
			Timestamp: time.Now().Unix(),
			StatusMessage: webhook.DataStatusMessage{
				IdMessage:       resp.ID,
				TimestampStatus: resp.Timestamp.Unix(),
				Status:          "sent",
			},
		}

		//отправляем вебхук
		webhook.SendStatusMessageWebhook(statusMessageWebhook, properties.InstanceWa.Log)
	}
}

// Метод обрабатывет сокет соединение
func wsHandle(ctx *gin.Context) {

	// обновляем HTTP протокол на websocket протокол
	conn, err := ws.Upgrader.Upgrade(ctx.Writer, ctx.Request, nil)

	// проверяем ошибку
	if err != nil {

		// отдаем ошибку
		http.NotFound(ctx.Writer, ctx.Request)

		// не продолжаем
		return
	}

	// создаем клиент ws
	clientWs := &ws.ClientWs{
		Socket: conn,
		Log:    properties.InstanceWa.Log,
	}

	// пишем клиента в инстанс
	properties.InstanceWa.WsQrClient = clientWs

	// запускаем чтение ws
	go properties.InstanceWa.WsQrClient.Read() //статичный метод

	// запускаем инстанс в отдельном потоке
	go properties.StartInstance()
}
