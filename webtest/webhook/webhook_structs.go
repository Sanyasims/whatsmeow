package webhook

// InstanceWhatsappWebhook Объект данных об инстансе Whatsapp
type InstanceWhatsappWebhook struct {
	IdInstance uint64 `json:"idInstance"`
	Wid        string `json:"wid"`
}
