package main

// Configuration Структура конфигурации
type Configuration struct {
	Host string `json:"host"`
}

// RequestSendMessage Структура отправки текстового сообщения
type RequestSendMessage struct {
	ChatId          string `json:"chatId"`
	Phone           int64  `json:"phone"`
	Message         string `json:"message"`
	QuotedMessageId string `json:"quotedMessageId"`
	IsForwarded     bool   `json:"isForwarded"`
}
