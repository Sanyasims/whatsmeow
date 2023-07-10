package properties

import (
	"fmt"
	"go.mau.fi/whatsmeow/socket"
	"net/http"
	"net/url"
	"strings"
)

// Configuration Структура конфигурации
type Configuration struct {
	Port      string `json:"port"`
	AppSecret string `json:"appSecret"`
}

// GetProxy метод получает прокси из строки
func GetProxy(proxy string) (socket.Proxy, error) {

	// проверяем входящие данные
	if proxy == "" {

		// отдаем ошибку
		return nil, fmt.Errorf("proxy is empty")
	}

	// разбиваем строку прокси
	parts := strings.Split(proxy, ":")

	// если не 4 части
	if len(parts) != 4 {

		// отдаем ошибку
		return nil, fmt.Errorf("bad proxy data")
	}

	// создаем URL
	proxyUrl := url.URL{
		Scheme: "socks5",
		Host:   parts[0] + ":" + parts[1],
		User:   url.UserPassword(parts[2], parts[3]),
	}

	// отдаем прокси
	return http.ProxyURL(&proxyUrl), nil
}

// DataMessage данные о сообщении
type DataMessage struct {
	ChatId           string
	MessageId        string
	MessageTimestamp uint64
	JsonData         string
	MessageStatus    int32
	StatusTimestamp  uint64
}
