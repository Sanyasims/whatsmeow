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
	StatusMessage    DataStatusMessage       `json:"statusMessage"`
}

// DataStatusMessage объект данных о статусе сообщения
type DataStatusMessage struct {
	IdMessage       string `json:"idMessage"`
	TimestampStatus int64  `json:"timestampStatus"`
	Status          string `json:"status"`
}

// SendStatusMessageWebhook Метод отправляет вебхук о статусе сообщения
func SendStatusMessageWebhook(statusMessageWebhook StatusMessageWebhook) {

	// сериализуем в JSON
	postBody, err := json.Marshal(statusMessageWebhook)

	// если ошибка
	if err != nil {

		// выводим лог
		log.Errorf("Error serialize status message %v", err)

		// не продолжаем
		return
	}

	// создаем тело запроса
	responseBody := bytes.NewBuffer(postBody)

	//отправляем запрос
	_, err = http.Post(statusMessageWebhook.WebhookUrl, "application/json", responseBody)

	// если ошибка
	if err != nil {

		// выводим лог
		log.Errorf("Error send status message webhook %v", err)
	}
}
