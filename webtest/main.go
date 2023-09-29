package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/gin-gonic/gin"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/webtest/formats"
	"go.mau.fi/whatsmeow/webtest/properties"
	"go.mau.fi/whatsmeow/webtest/wainstance"
	"go.mau.fi/whatsmeow/webtest/webhook"
	"go.mau.fi/whatsmeow/webtest/ws"
	"io"
	"net/http"
	"os"
	"runtime"
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

	osType := runtime.GOOS

	wainstance.InstanceWa.Log.Infof("Run app on: %s", osType)

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

	// запуск инстанса
	engine.POST("/startInstance", startInstance)

	// остановка инстанса
	engine.GET("/stopInstance", stopInstance)

	// сокет соединение авторизации
	engine.GET("/ws", wsHandle)

	// авторизация GET запросом
	engine.POST("/getQrCode", getQrCode)

	// маршрутизация отправки сообщения
	engine.POST("/sendMessage", sendMessage)

	// получение контактов
	engine.GET("/getContacts", getContacts)

	// проверка номера на наличие аккаунта Whatsapp
	engine.POST("/checkWhatsapp", checkWhatsapp)

	// разлогинивание инстанса
	engine.GET("/logoutInstance", logoutInstance)

	// получение аватара аккаунта Whatsapp
	engine.POST("/getProfilePicture", getProfilePicture)

	// получение статуса акаунта Whatsapp
	engine.POST("/getStatusAccount", getStatusAccount)

	// установка webhook URL
	engine.POST("/setWebhookUrl", setWebhookUrl)

	// если os windows
	if osType == "windows" {

		// запускаем сервер
		err = engine.Run("127.0.0.1:" + wainstance.InstanceWa.Config.Port)
	} else {

		// запускаем сервер
		err = engine.Run("0.0.0.0:" + wainstance.InstanceWa.Config.Port)
	}

	// если есть ошибка
	if err != nil {

		// выводим лог
		wainstance.InstanceWa.Log.Errorf("Failed to start server: %v", err)

		//не продолжаем
		return
	}
}

// Метод проверяет подключен ли инстанс и авторизован
func isConnectAndAuth() bool {

	// если клиент nil или не установлено сокет соединение с Whatsapp или инстанс не авторизован
	if wainstance.InstanceWa.Client == nil || !wainstance.InstanceWa.Client.IsConnected() || !wainstance.InstanceWa.Client.IsLoggedIn() {

		// отдаем инстанс не подключен
		return false
	}

	// отдаем инстанс подключен и авторизован
	return true
}

// Метод проверяет валидность запроса
func isValidRequest(ctx *gin.Context) bool {

	// если не проверяем секретный заголовок
	if !wainstance.InstanceWa.Config.CheckSecret {

		// отдаем что щапрос валиден
		return true
	}

	// получаем заголовки
	appSecret := ctx.Request.Header["App-Secret"]

	// если нет заголовоков
	if len(appSecret) < 1 {

		// отдаем false
		return false
	}

	// отдаем сравнение заголовков
	return appSecret[0] == wainstance.InstanceWa.Config.AppSecret
}

// Метод запускает инстанс
func startInstance(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

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

	//пишем webhook URL
	wainstance.InstanceWa.WebhookUrl = requestRunInstance.WebhookUrl

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

// Метод останавливает инстанс
func stopInstance(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

	// запускаем инстанс в отдельном потоке
	go wainstance.StopInstance()

	// отдаем ответ
	ctx.JSON(200, gin.H{
		"success": true,
	})
}

// Метод обрабатывет сокет соединение авторизации
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

	// если нужно проверять секретный заголовок
	if wainstance.InstanceWa.Config.CheckSecret {

		// получаем параметр прокси
		queryAppSecret, ok := ctx.GetQuery("app_secret")

		//если не ок или нет параметра
		if !ok || queryAppSecret == "" || queryAppSecret != wainstance.InstanceWa.Config.AppSecret {

			wainstance.InstanceWa.WsQrClient.Send(ws.AuthMessage{
				Type:   "error",
				Reason: "Bad app secret",
			})

			wainstance.InstanceWa.WsQrClient.Close()

			//не продолжаем
			return
		}
	}

	// получаем параметр прокси
	queryProxy, ok := ctx.GetQuery("proxy")

	//если не ок или нет параметра
	if !ok || queryProxy == "" {

		wainstance.InstanceWa.WsQrClient.Send(ws.AuthMessage{
			Type:   "error",
			Reason: "Missing proxy",
		})

		wainstance.InstanceWa.WsQrClient.Close()

		//не продолжаем
		return
	}

	// получаем прокси из строки
	proxy, err := properties.GetProxy(queryProxy)

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

// Метод получает QR код авторизации GET запросом
func getQrCode(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

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

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

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

	// проверяем клиента
	if wainstance.InstanceWa.Client == nil {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance not running",
		})

		// не продолжаем
		return
	}

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
	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(requestSendMessage.Message),
		},
	}

	// добавляем идентификатор сообщения
	extra := whatsmeow.SendRequestExtra{
		ID: requestSendMessage.Id,
	}

	// отправляем сообщение
	resp, err := wainstance.InstanceWa.Client.SendMessage(context.Background(), recipient, msg, extra)

	// если есть ошибка
	if err != nil {

		// выводим ошибку
		wainstance.InstanceWa.Log.Errorf("Error sending message: %v", err)

		// отдаем ответ
		ctx.JSON(500, gin.H{
			"reason": "Error sending message: " + err.Error(),
		})

	} else {

		// выводим лог
		wainstance.InstanceWa.Log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)

		// отдаем ответ
		ctx.JSON(200, gin.H{
			"id": resp.ID,
		})

		// сериализуем сообщение
		jsonData, err := json.Marshal(msg)

		// создаем объект сообщения
		dataMessage := properties.DataMessage{
			ChatId:           recipient.String(),
			MessageId:        resp.ID,
			MessageTimestamp: uint64(resp.Timestamp.Unix()),
			JsonData:         string(jsonData),
			MessageStatus:    1,
			StatusTimestamp:  uint64(resp.Timestamp.Unix()),
		}

		// сохраняем сообщение в историю
		err = wainstance.InstanceWa.Client.HistorySync([]properties.DataMessage{dataMessage})

		// если ошибка
		if err != nil {

			// выводим ошибку
			wainstance.InstanceWa.Log.Errorf("error HistorySync %v", err)
		}

		// создаем структуру вебхук о статусе сообщения
		statusMessageWebhook := webhook.StatusMessageWebhook{
			TypeWebhook:     "statusMessage",
			WebhookUrl:      wainstance.InstanceWa.WebhookUrl,
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
		statusMessageWebhook.SendStatusMessageWebhook(wainstance.InstanceWa.Log)
	}
}

// Метод отдает контакты инстанса
func getContacts(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

	// если инстнанс не подключен, либо не авторизован
	if !isConnectAndAuth() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance not connected or not auth",
		})

		// не продолжаем
		return
	}

	// получаем контакты
	contacts, err := wainstance.InstanceWa.Client.Store.Contacts.GetAllContacts()

	//если ошибка
	if err != nil {
		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error get contacts: %v", err)
	}

	// отдаем ответ
	ctx.JSON(200, contacts)
}

// Метод проверяет номер на наличие аккаунта Whatsapp
func checkWhatsapp(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

	// если инстнанс не подключен, либо не авторизован
	if !isConnectAndAuth() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance not connected or not auth",
		})

		// не продолжаем
		return
	}

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

	// объявляем структуру запроса с номером телефона
	var requestWithPhoneNumber properties.RequestWithPhoneNumber

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestWithPhoneNumber)

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

	// проверяем на Whatsapp
	resp, err := wainstance.InstanceWa.Client.IsOnWhatsApp([]string{requestWithPhoneNumber.Phone})

	// если ошибка
	if err != nil {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": err.Error(),
		})
	} else {

		responseCheckWhatsapp := properties.ResponseCheckWhatsapp{}

		// обходим ответ
		for _, item := range resp {

			if item.VerifiedName != nil {

				responseCheckWhatsapp.WhatsappOnPhone = item.IsIn

				responseCheckWhatsapp.IsBusiness = true

				wainstance.InstanceWa.Log.Infof("%s: on whatsapp: %t, JID: %s, business name: %s", item.Query, item.IsIn, item.JID, item.VerifiedName.Details.GetVerifiedName())

			} else {

				responseCheckWhatsapp.WhatsappOnPhone = item.IsIn

				wainstance.InstanceWa.Log.Infof("%s: on whatsapp: %t, JID: %s", item.Query, item.IsIn, item.JID)
			}
		}

		// отдаем ответ
		ctx.JSON(200, responseCheckWhatsapp)
	}
}

// Метод разлогинивает инстанс
func logoutInstance(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

	// если инстнанс не подключен, либо не авторизован
	if !isConnectAndAuth() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance not connected or not auth",
		})

		// не продолжаем
		return
	}

	// разлогиваем инстанс
	err := wainstance.InstanceWa.Client.Logout()

	// если ошибка
	if err != nil {

		// отдаем ответ
		ctx.JSON(200, gin.H{
			"success": false,
			"reason":  err.Error(),
		})

	} else {

		// отдаем ответ
		ctx.JSON(200, gin.H{
			"success": true,
		})
	}
}

// Метод получает аватар аккаунта Whatsapp
func getProfilePicture(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

	// если инстнанс не подключен, либо не авторизован
	if !isConnectAndAuth() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance not connected or not auth",
		})

		// не продолжаем
		return
	}

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

	// объявляем структуру запроса с номером телефона
	var requestWithPhoneNumber properties.RequestWithPhoneNumber

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestWithPhoneNumber)

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

	// парсим JID
	jid, ok := wainstance.ParseJID(requestWithPhoneNumber.Phone)

	// если не ок
	if !ok {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Error parse JID",
		})

		// не продолжаем
		return
	}

	// получаем профиль
	pic, err := wainstance.InstanceWa.Client.GetProfilePictureInfo(jid, &whatsmeow.GetProfilePictureParams{
		Preview:     false,
		IsCommunity: false,
		ExistingID:  "",
	})

	if err != nil {

		wainstance.InstanceWa.Log.Errorf("Failed to get avatar: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Failed to get avatar: " + err.Error(),
		})

	} else if pic != nil {

		// отдаем ответ
		ctx.JSON(200, gin.H{
			"avatarUrl": pic.URL,
		})

	} else {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "No avatar found",
		})
	}
}

// Метод получает статус аккаунта Whatsapp
func getStatusAccount(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

	// если инстнанс не подключен, либо не авторизован
	if !isConnectAndAuth() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance not connected or not auth",
		})

		// не продолжаем
		return
	}

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

	// объявляем структуру запроса с номером телефона
	var requestWithPhoneNumber properties.RequestWithPhoneNumber

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestWithPhoneNumber)

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

	// создаем канал связки потоков
	wainstance.InstanceWa.ChainResponseGetStatusAccount = make(chan properties.ResponseGetStatusAccount)

	// делаем себя недоступным
	err = wainstance.SetPresence("unavailable")

	// если ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error SetPresence: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Error SetPresence: " + err.Error(),
		})

		// не продолжаем
		return
	}

	// делаем задержку
	time.Sleep(300 * time.Millisecond)

	//делаем себя доступным
	err = wainstance.SetPresence("available")

	// если ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error SetPresence: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Error SetPresence: " + err.Error(),
		})

		// не продолжаем
		return
	}

	// подписываемся на пользователя
	err = wainstance.SubscribePresence(requestWithPhoneNumber.Phone)

	// если ошибка
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error SubscribePresence: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Error SubscribePresence: " + err.Error(),
		})

		// не продолжаем
		return
	}

	// получаем из канала данные о пользователе
	responseGetStatusAccount := <-wainstance.InstanceWa.ChainResponseGetStatusAccount

	// ставим nil каналу
	wainstance.InstanceWa.ChainResponseGetStatusAccount = nil

	// получаем данные пользователя
	resp, err := wainstance.GetUser(requestWithPhoneNumber.Phone)

	// делаем себя недоступным
	if err != nil {

		// логируем ошибку
		wainstance.InstanceWa.Log.Errorf("Error GetUser: %v", err)

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Error GetUser: " + err.Error(),
		})

		// не продолжаем
		return
	}

	// обходим ответ
	for _, info := range resp {

		// пишем статус аккаунта
		responseGetStatusAccount.StatusAccount = info.Status

		// пишем время установки статуса аккаунта
		responseGetStatusAccount.TimeStatusSet = formats.ParseInt64(info.TimeStatusSet)
	}

	// отдаем ответ
	ctx.JSON(200, responseGetStatusAccount)
}

// Метод устанавливает webhook URL
func setWebhookUrl(ctx *gin.Context) {

	// если запрос не валиден
	if !isValidRequest(ctx) {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Bad request header",
		})

		// не продолжаем
		return
	}

	// если инстнанс не подключен, либо не авторизован
	if !isConnectAndAuth() {

		// отдаем ответ
		ctx.JSON(400, gin.H{
			"reason": "Instance not connected or not auth",
		})

		// не продолжаем
		return
	}

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

	// объявляем структуру запроса установки Webhook URL
	var requestSetWebhookUrl properties.RequestSetWebhookUrl

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestSetWebhookUrl)

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

	//пишем webhook URL
	wainstance.InstanceWa.WebhookUrl = requestSetWebhookUrl.WebhookUrl

	// отдаем ответ
	ctx.JSON(200, gin.H{
		"success": true,
	})
}
