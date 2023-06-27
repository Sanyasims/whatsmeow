package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/gin-gonic/gin"
	"go.mau.fi/whatsmeow/webtest/properties"
	"go.mau.fi/whatsmeow/webtest/wainstance"
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
	wainstance.InstanceWa = wainstance.Instance{
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

	if *wainstance.InstanceWa.RequestFullSync {

		store.DeviceProps.RequireFullSync = proto.Bool(true)
	}

	wainstance.InstanceWa.Log.Infof("Run app")

	// считываем файл кофигурации
	content, err := os.ReadFile("config.json")

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error when opening config file: %v", err)

		// не продолжаем
		return
	}

	// лесериализуем из JSON
	err = json.Unmarshal(content, &wainstance.InstanceWa.Config)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error during parse Configuration: %v", err)

		// не продолжаем
		return
	}

	// создаем экземпляр Engine
	engine := gin.Default()

	// маршрутизация запуска инстанса
	engine.GET("/runInstance", runInstance)

	// websocket
	engine.GET("/ws", wsHandle)

	// метод получает QR код авторизации GET запросом
	engine.POST("getQrCode", getQrCode)

	// маршрутизация отправки сообщения
	engine.POST("/sendMessage", sendMessage)

	// запускаем сервер
	err = engine.Run(wainstance.InstanceWa.Config.Host)

	// если есть ошибка
	if err != nil {

		// выводим лог
		wainstance.InstanceWa.Log.Errorf("Failed to start server: %v", err)

		//не продолжаем
		return
	}
}

// Метод запускает инстанс
func runInstance(ctx *gin.Context) {

	// считываем тело запроса
	content, err := io.ReadAll(ctx.Request.Body)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error read body request: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		// не продолжаем
		return
	}

	// объявляем структуру запроса запуска инстанса
	var requestRunInstance properties.RequestRunInstance

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestRunInstance)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error during parse RequestRunInstance: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		// не продолжаем
		return
	}

	// если не указано прокси
	if requestRunInstance.Proxy == "" {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Missing proxy RequestRunInstance: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Missing proxy",
		})

		// не продолжаем
		return
	}

	// получаем прокси из строки
	proxy, err := properties.GetProxy(requestRunInstance.Proxy)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error get proxy from string: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": err.Error(),
		})

		// не продолжаем
		return
	}

	// если клиент не nil и установлено сокет соединение с Whatsapp
	if wainstance.InstanceWa.Client != nil && wainstance.InstanceWa.Client.IsConnected() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance already connected",
		})

		// не продолжаем
		return
	}

	// запускаем инстанс в отдельном потоке
	go wainstance.StartInstance(proxy, true)

	// отдаем ответ
	ctx.JSON(200, gin.H{
		"success": true,
	})
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
		Log:    wainstance.InstanceWa.Log,
	}

	// пишем клиента в инстанс
	wainstance.InstanceWa.WsQrClient = clientWs

	// получаем параметр прокси
	query, ok := ctx.GetQuery("proxy")

	//если не ок или нет параметра
	if !ok || query == "" {

		wainstance.InstanceWa.WsQrClient.Send(ws.AuthMessage{
			Type:   "error",
			Reason: "Missing proxy",
		})

		wainstance.InstanceWa.WsQrClient.Close()

		//не продолжаем
		return
	}

	// получаем прокси из строки
	proxy, err := properties.GetProxy(query)

	// если есть ошибка
	if err != nil {

		wainstance.InstanceWa.WsQrClient.Send(ws.AuthMessage{
			Type:   "error",
			Reason: "Error get proxy from string",
		})

		wainstance.InstanceWa.WsQrClient.Close()

		//не продолжаем
		return
	}

	// если клиент не nil и установлено сокет соединение с Whatsapp
	if wainstance.InstanceWa.Client != nil && wainstance.InstanceWa.Client.IsConnected() {

		wainstance.InstanceWa.WsQrClient.Send(ws.AuthMessage{
			Type:   "error",
			Reason: "Instance already connected",
		})

		wainstance.InstanceWa.WsQrClient.Close()

		// не продолжаем
		return
	}

	// запускаем чтение ws
	go wainstance.InstanceWa.WsQrClient.Read() //статичный метод

	// запускаем инстанс в отдельном потоке
	go wainstance.StartInstance(proxy, false)
}

// wsHandle получает QR код авторизации GET запросом
func getQrCode(ctx *gin.Context) {

	// считываем тело запроса
	content, err := io.ReadAll(ctx.Request.Body)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error read body request: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		// не продолжаем
		return
	}

	// объявляем структуру запроса запуска инстанса
	var requestRunInstance properties.RequestRunInstance

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestRunInstance)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error during parse RequestRunInstance: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		// не продолжаем
		return
	}

	// если не указано прокси
	if requestRunInstance.Proxy == "" {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Missing proxy RequestRunInstance: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Missing proxy",
		})

		// не продолжаем
		return
	}

	// получаем прокси из строки
	proxy, err := properties.GetProxy(requestRunInstance.Proxy)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error get proxy from string: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": err.Error(),
		})

		// не продолжаем
		return
	}

	// если еще нет сообщения авторизации
	if wainstance.InstanceWa.Client != nil && wainstance.InstanceWa.Client.AuthMessage != nil {

		// отдаем ответ
		ctx.JSON(200, wainstance.InstanceWa.Client.AuthMessage)

		// если тип сообщения ошибка ил данные аккаунта
		if wainstance.InstanceWa.Client.AuthMessage.Type == "error" || wainstance.InstanceWa.Client.AuthMessage.Type == "account" {

			// пишем nil сообщению авторизации
			wainstance.InstanceWa.Client.AuthMessage = nil
		}

		// не продолжаем
		return
	}

	// если клиент не nil и установлено сокет соединение с Whatsapp
	if wainstance.InstanceWa.Client != nil && wainstance.InstanceWa.Client.IsConnected() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance already connected",
		})

		// не продолжаем
		return
	}

	// запускаем инстанс в отдельном потоке
	go wainstance.StartInstance(proxy, false)

	// счетчик итераций
	ePoch := 0

	for {

		// если еще нет сообщения авторизации
		if wainstance.InstanceWa.Client == nil || wainstance.InstanceWa.Client.AuthMessage == nil {

			// делаем задержку
			time.Sleep(500 * time.Millisecond)

			// инкременируем счетчик итераций
			ePoch++

		} else {

			// отдаем ответ
			ctx.JSON(200, wainstance.InstanceWa.Client.AuthMessage)

			// прерываем цикл
			return
		}
	}
}

// Метод отправляет сообщение
func sendMessage(ctx *gin.Context) {

	// считываем тело запроса
	content, err := io.ReadAll(ctx.Request.Body)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error read body request: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		// не продолжаем
		return
	}

	// объявляем структуру отправки текстового сообщения
	var requestSendMessage properties.RequestSendMessage

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestSendMessage)

	// если есть ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error during parse RequestSendMessage: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		// не продолжаем
		return
	}

	//TODO проверять валидность данных

	// парсим идентифкатор Whatsapp, если chatId то его
	recipient, ok := wainstance.ParseJID(strconv.FormatInt(requestSendMessage.Phone, 10))

	// если не ок
	if !ok {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		// не продолжаем
		return
	}

	// кодируем сообщение
	msg := &waProto.Message{Conversation: proto.String(requestSendMessage.Message)}

	// отправляем сообщение
	resp, err := wainstance.InstanceWa.Client.SendMessage(context.Background(), recipient, msg)

	// если есть ошибка
	if err != nil {

		// выводим ошибку
		wainstance.InstanceWa.Log.Errorf("Error sending message: %v", err)

		// отдаем ответ
		ctx.JSON(500, gin.H{
			"reason": "Error sending message",
		})

	} else {

		// выводим лог
		wainstance.InstanceWa.Log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)

		// отдаем ответ
		ctx.JSON(200, gin.H{
			"id": resp.ID,
		})

		// создаем структуру вебхук о статусе сообщения
		statusMessageWebhook := webhook.StatusMessageWebhook{
			TypeWebhook:     "statusMessage",
			WebhookUrl:      wainstance.InstanceWa.Config.WebhookUrl,
			CountTrySending: 0,
			InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
				IdInstance: 0,
				Wid:        wainstance.InstanceWa.Client.Store.ID.User + "@c.us",
			},
			Timestamp: time.Now().Unix(),
			StatusMessage: webhook.DataStatusMessage{
				IdMessage:       resp.ID,
				TimestampStatus: resp.Timestamp.Unix(),
				Status:          "sent",
			},
		}

		// отправляем вебхук
		webhook.SendStatusMessageWebhook(statusMessageWebhook, wainstance.InstanceWa.Log)
	}
}
