package webhook

import (
	"bytes"
	"encoding/json"
	waLog "go.mau.fi/whatsmeow/util/log"
	"net/http"
)

// NewMessageWebhook объект данных webhook о новом сообщении
type NewMessageWebhook struct {
	TypeWebhook      string                  `json:"type"`
	WebhookUrl       string                  `json:"-"`
	CountTrySending  uint32                  `json:"-"`
	InstanceWhatsapp InstanceWhatsappWebhook `json:"instanceWhatsapp"`
	Timestamp        int64                   `json:"timestamp"`
	NewMessage       NewMessage              `json:"newMessage"`
}

// NewMessage объект данных о сообщении
type NewMessage struct {
	Chat             ChatDataWhatsappMessage `json:"chat"`
	Message          DataWhatsappMessage     `json:"message"`
	MessageTimestamp int64                   `json:"messageTimestamp"`
	Status           string                  `json:"status"`
}

// ChatDataWhatsappMessage объект данных о чате сообщения
type ChatDataWhatsappMessage struct {
	ChatId    string `json:"chatId"`
	FromMe    bool   `json:"fromMe"`
	IdMessage string `json:"idMessage"`
}

// DataWhatsappMessage объект данных сообщения Whatsapp
type DataWhatsappMessage struct {
	TypeMessage string `json:"typeMessage"`
	Text        string `json:"text"`
}

// SendNewMessageWebhook Метод отправляет вебхук о новом входящем сообщении
func SendNewMessageWebhook(newMessageWebhook NewMessageWebhook, log waLog.Logger) {

	// сериализуем в JSON
	postBody, err := json.Marshal(newMessageWebhook)

	// если ошибка
	if err != nil {

		// выводим лог
		log.Errorf("Error serialize new incoming message %v", err)

		// не продолжаем
		return
	}

	// создаем тело запроса
	responseBody := bytes.NewBuffer(postBody)

	req, err := http.NewRequest("POST", newMessageWebhook.WebhookUrl, responseBody)

	if err != nil {

		// выводим лог
		log.Errorf("Error NewRequest %v", err)

		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PostmanRuntime/7.32.3")

	client := http.Client{}

	res, err := client.Do(req)

	// если ошибка
	if err != nil {

		// выводим лог
		log.Errorf("Error send status message webhook %v", err)
	}

	log.Infof("Response status: %d", res.StatusCode)
}
