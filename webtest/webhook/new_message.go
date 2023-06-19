package webhook

import (
	"bytes"
	"encoding/json"
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
func SendNewMessageWebhook(newMessageWebhook NewMessageWebhook) {

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

	//отправляем запрос
	_, err = http.Post(newMessageWebhook.WebhookUrl, "application/json", responseBody)

	// если ошибка
	if err != nil {

		// выводим лог
		log.Errorf("Error send new incoming message webhook %v", err)
	}
}
