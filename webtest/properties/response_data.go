package properties

// ResponseCheckWhatsapp объект ответа на проверку аккаунта Whatsapp
type ResponseCheckWhatsapp struct {
	WhatsappOnPhone bool `json:"whatsappOnPhone"`
	IsBusiness      bool `json:"isBusiness"`
}

// ResponseGetStatusAccount объект ответа на получение информации о пользователе
type ResponseGetStatusAccount struct {
	StatusAvailable string `json:"statusAvailable"`
	LastVisit       uint64 `json:"lastVisit"`
	StatusAccount   string `json:"statusAccount"`
	TimeStatusSet   uint64 `json:"timeStatusSet"`
}
