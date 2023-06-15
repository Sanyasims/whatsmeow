package properties

// RequestSendMessage Структура отправки текстового сообщения
type RequestSendMessage struct {
	ChatId          string `json:"chatId"`
	Phone           int64  `json:"phone"`
	Message         string `json:"message"`
	QuotedMessageId string `json:"quotedMessageId"`
	IsForwarded     bool   `json:"isForwarded"`
}
