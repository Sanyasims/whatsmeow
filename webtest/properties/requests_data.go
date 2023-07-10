package properties

// RequestRunInstance структура запроса запуска инстанса
type RequestRunInstance struct {
	Proxy string `json:"proxy"`
}

// RequestSendMessage Структура отправки текстового сообщения
type RequestSendMessage struct {
	ChatId          string `json:"chatId"`
	Phone           int64  `json:"phone"`
	Message         string `json:"message"`
	QuotedMessageId string `json:"quotedMessageId"`
	IsForwarded     bool   `json:"isForwarded"`
}

// RequestWithPhoneNumber Структура запроса с номером телефона
type RequestWithPhoneNumber struct {
	Phone string `json:"phone"`
}

// RequestSetWebhookUrl Структура запроса установки Webhook URL
type RequestSetWebhookUrl struct {
	WebhookUrl string `json:"webhookUrl"`
}
