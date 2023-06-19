package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"

	waLog "go.mau.fi/whatsmeow/util/log"
)

var log waLog.Logger

// StatusMessageWebhook объект данных webhook о статусе сообщения
type StatusMessageWebhook struct {
	TypeWebhook      string                  `json:"type"`
	WebhookUrl       string                  `json:"-"`
	CountTrySending  uint32                  `json:"-"`
	InstanceWhatsapp InstanceWhatsappWebhook `json:"instanceWhatsapp"`
	Timestamp        int64                   `json:"timestamp"`
	StatusMessage    string                  `json:"statusMessage"`
}

// SendStatusMessage Метод отправляет вебхук о статусе сообщения
func SendStatusMessage(statusMessageWebhook StatusMessageWebhook) {

	// сериализуем в JSON
	postBody, err := json.Marshal(statusMessageWebhook)

	if err != nil {

		log.Errorf("Error serialize message %v", err)

		return
	}

	// создаем тело запроса
	responseBody := bytes.NewBuffer(postBody)

	//отправляем запрос
	_, err = http.Post(statusMessageWebhook.WebhookUrl, "application/json", responseBody)

	if err != nil {
		log.Errorf("Error send status message %v", err)
	}
}
