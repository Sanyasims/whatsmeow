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
	Host  string `json:"host"`
	Proxy string `json:"proxy"`
}

// GetProxy метод получает прокси из конфинурации
func (config Configuration) GetProxy() (socket.Proxy, error) {

	// проверяем входящие данные
	if config.Proxy == "" {

		// отдаем ошибку
		return nil, fmt.Errorf("proxy is empty")
	}

	// разбиваем строку прокси
	parts := strings.Split(config.Proxy, ":")

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
