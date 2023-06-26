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

// Метод обрабатывает сокет соединение
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
