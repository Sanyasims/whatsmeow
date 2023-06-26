package ws

import (
	"github.com/gorilla/websocket"
	waLog "go.mau.fi/whatsmeow/util/log"
	"net/http"
)

// Upgrader обновляет HTTP протокол на websocket протокол
var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 1024 * 1024,
	WriteBufferSize: 1024 * 1024 * 1024,
	CheckOrigin: func(r *http.Request) bool { //Cors
		return true
	},
}

// Client ws клиент
type Client struct {
	Socket *websocket.Conn //Connected socket
	Log    waLog.Logger
}

// DataWs данные ws сообщения
type DataWs struct {
	Type        string `json:"type"`
	ImageQrCode string `json:"imageQrCode"`
	Reason      string `json:"reason"`
}

// Read Метод обрабатывает сокет соединение
func (client *Client) Read() {

	// создаем отложенную функцию
	defer func() {

		//закрываем сокет соединение
		_ = client.Socket.Close()
	}()

	for {

		// считываем сообщение
		messageType, p, err := client.Socket.ReadMessage()

		// если ошибка
		if err != nil {

			// выводим ошибку
			client.Log.Errorf("Error ReadMessage: ", err)

			// не продолжаем
			return
		}

		// приводим сообщение в строку
		message := string(p)

		// выводим лог с сообщением
		client.Log.Infof(message)

		// смотрим собщение
		switch message {
		case "__ping__": //если ping

			// отправляем сообщение в ответ
			if err := client.Socket.WriteMessage(messageType, []byte("__pong__")); err != nil {

				// если есть ошибка, выводим ее
				client.Log.Errorf("Error WriteMessage: ", err)

				// не продолжаем
				return
			}
		}
	}
}

// Send Метод отправляет сокет сообщение
func (client *Client) Send(data DataWs) (success bool) {

	// если ws не инициализирован
	if client.Socket == nil {

		// если есть ошибка, выводим ее
		client.Log.Errorf("client.Socket is nil")

		// отдаем не отправлено
		return false
	}

	// отправляем сообщение в ответ
	if err := client.Socket.WriteJSON(data); err != nil {

		// если есть ошибка, выводим ее
		client.Log.Errorf("Error WriteMessage: ", err)

		// отдаем не отправлено
		return false
	}

	return true
}
