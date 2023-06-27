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

// ClientWs ws клиент
type ClientWs struct {
	Socket *websocket.Conn //Connected socket
	Log    waLog.Logger
}

// DataWs данные ws сообщения
type DataWs struct {
	Type        string `json:"type"`
	ImageQrCode string `json:"imageQrCode"`
	Reason      string `json:"reason"`
	Pushname    string `json:"pushname"`
	Wid         string `json:"wid"`
}

// Read Метод обрабатывает сокет соединение
func (clientWs *ClientWs) Read() {

	// создаем отложенную функцию
	defer func() {

		//закрываем сокет соединение
		clientWs.Close()
	}()

	for {

		// считываем сообщение
		messageType, p, err := clientWs.Socket.ReadMessage()

		// если ошибка
		if err != nil {

			// выводим ошибку
			clientWs.Log.Errorf("Error ReadMessage: %v", err)

			// не продолжаем
			return
		}

		// приводим сообщение в строку
		message := string(p)

		// выводим лог с сообщением
		clientWs.Log.Infof(message)

		// смотрим собщение
		switch message {
		case "__ping__": //если ping

			// отправляем сообщение в ответ
			if err := clientWs.Socket.WriteMessage(messageType, []byte("__pong__")); err != nil {

				// если есть ошибка, выводим ее
				clientWs.Log.Errorf("Error WriteMessage: %v", err)

				// не продолжаем
				return
			}
		}
	}
}

// Send Метод отправляет сокет сообщение
func (clientWs *ClientWs) Send(data DataWs) (success bool) {

	// если ws не инициализирован
	if clientWs == nil || clientWs.Socket == nil {

		// если есть ошибка, выводим ее
		clientWs.Log.Errorf("clientWs.Socket is nil")

		// отдаем не отправлено
		return false
	}

	// отправляем сообщение в ответ
	if err := clientWs.Socket.WriteJSON(data); err != nil {

		// если есть ошибка, выводим ее
		clientWs.Log.Errorf("Error WriteMessage: %v", err)

		// отдаем не отправлено
		return false
	}

	// отдаем отправлено
	return true
}

// Close Метод закрывает сокет сообщение
func (clientWs *ClientWs) Close() {

	//закрываем сокет соединение
	err := clientWs.Socket.Close()

	if err != nil {

		// если есть ошибка, выводим ее
		clientWs.Log.Errorf("Error WriteMessage: %v", err)
	}
}
