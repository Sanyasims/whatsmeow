package webhook

import (
	"bytes"
	"encoding/json"
	waLog "go.mau.fi/whatsmeow/util/log"
	"net/http"
)

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
func (statusMessageWebhook *StatusMessageWebhook) SendStatusMessageWebhook(log waLog.Logger) {

	// если вебхук не установлен
	if statusMessageWebhook.WebhookUrl == "" {

		// не продолжаем
		return
	}

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

	req, err := http.NewRequest("POST", statusMessageWebhook.WebhookUrl, responseBody)

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
