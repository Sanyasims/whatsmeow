package ws

import (
	"github.com/gorilla/websocket"
	waLog "go.mau.fi/whatsmeow/util/log"
	"net/http"
)

// Upgrader обновитель сокет соединения
var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 1024 * 1024,
	WriteBufferSize: 1024 * 1024 * 1024,
	CheckOrigin: func(r *http.Request) bool { //Solving cross-domain problems
		return true
	},
}

// Client ws клиент
type Client struct {
	Socket *websocket.Conn //Connected socket
}

// Message into JSON
type Message struct {
	//Message Struct
	Sender    string `json:"sender,omitempty"`    //发送者
	Recipient string `json:"recipient,omitempty"` //接收者
	Content   string `json:"content,omitempty"`   //内容
	ServerIP  string `json:"serverIp,omitempty"`  //实际不需要 验证k8s
	SenderIP  string `json:"senderIp,omitempty"`  //实际不需要 验证k8s
}

// Define the read method of the client structure
func (client *Client) Read(log waLog.Logger) {

	defer func() {
		_ = client.Socket.Close()
	}()

	for {

		// read in a message
		messageType, p, err := client.Socket.ReadMessage()

		if err != nil {

			log.Errorf("Error ReadMessage: ", err)

			return
		}

		// приводим сообщение в строку
		message := string(p)

		// print out that message for clarity
		log.Infof(message)

		switch message {
		case "__ping__":

			if err := client.Socket.WriteMessage(messageType, []byte("__pong__")); err != nil {

				log.Errorf("Error WriteMessage: ", err)

				return
			}
		}
	}
}
