/**
 * Класс авторизации инстанса Whatsapp
 */
class InstanceAuthorization {

	/**
	 * Конструктор
	 */
	constructor() {

		//получаем изображение QR кода
		this.imageQrCode = document.querySelector(".qr_code_image > img");

		//получаем заглушку QR кода
		this.plugQrCode = document.querySelector(".plug_qr_code");

		//получаем иконку Whatsapp
		this.iconWhatsapp = document.querySelector(".icon_whatsapp");

		//получаем кнопку сканировать QR код
		this.btnScanQrCode = document.querySelector(".btn.scan_qr");

		//создаем loader кнопки сканировать QR код
		this.loaderButton = new Loader(this.btnScanQrCode);

		//ставим null websocket
		this.webSocket = null;
	}

	/**
	 * Метод запускает авторизацию инстанса Whatsapp
	 */
	startInstanceAuthorization() {

		//проверяем данные
		if (!this.imageQrCode || !this.plugQrCode || !this.iconWhatsapp || !this.btnScanQrCode) return;

		//привязываем метод на клик по кнопке сканировать QR код
		this.btnScanQrCode.addEventListener("click", () => this.getQrCode());
	}

	/**
	 * Метод запускает получение QR кода
	 */
	getQrCode() {

		//закрываем кнопку
		this.loaderButton.setDisable();

		//устанавливаем сокет соединение
		this.webSocket = new WebSocket(`ws://127.0.0.1:10001/ws`);

		//открытие соединения
		this.webSocket.onopen = () => {

			//выводим лог
			console.log("Connection established");

			//запускаем таймер keep alive
			this.keepAliveTimer = setInterval(() => {

				//отправляем ping
				this.webSocket.send("__ping__");

			}, 30000);
		};

		//закрытие соединения
		this.webSocket.onclose = event => {

			//если чистое закрытие
			if (event.wasClean) {

				//выводим лог
				console.log("Connection closed cleanly");

			} else {

				//выводим лог
				console.log("Lost connection");
			}

			//выводим лог
			console.log(`code: ${event.code} reason: ${event.reason}`);

			//убираем панель QR кода
			this.toggleControls(false);

			//сбрасываем таймер keep alive
			clearInterval(this.keepAliveTimer);
		};

		//ошибка
		this.webSocket.onerror = error => {

			//выводим ошибку на консоль
			console.log(`Error: ${error.message}`);

			//убираем панель QR кода
			this.toggleControls(false);
		};

		//получение сообщений
		this.webSocket.onmessage = event => {

			try {

				//проверяем полученные данные
				if (!event.data || event.data === "__pong__") return;

				//десериализуем ответ из JSON
				const dataMessage = JSON.parse(event.data);

				//смотрим тип сообщения
				switch (dataMessage.type) {

					case "error": //ошибка
					{
						//выводим лог
						console.log(dataMessage.reason)

						//убираем панель QR кода
						this.toggleControls(false);
					}
						return;
					case "account": //данные авторизованного инстанса Whatsapp
					{
						//убираем панель QR кода
						this.toggleControls(false);
					}
						return;
					case "qr": //изображение QR кода
					{
						//выводим изображение QR кода
						this.imageQrCode.src = dataMessage.imageQrCode;

						//открываем панель QR кода
						this.toggleControls(true);
					}
						return;
				}

			} catch (exception) {

				//выводим ошибку на консоль
				console.error("Error: ", exception);
			}
		};
	}

	/**
	 * Метод переключает контролы формы
	 * @param qrCodeReceived - флаг получено ли изображение QR кода
	 */
	toggleControls(qrCodeReceived) {

		//если изображение QR кода получено
		if (qrCodeReceived) {

			//делаем активным изображение QR кода
			this.imageQrCode.classList.add("active");

			//прячем заглушку QR кода
			this.plugQrCode.classList.remove("active");

			//делаем активной иконку Whatsapp
			this.iconWhatsapp.classList.add("active");

		} else {

			//прячем изображение QR кода
			this.imageQrCode.classList.remove("active");

			//убираем изображение QR кода
			this.imageQrCode.src = "";

			//делаем активной заглушку QR кода
			this.plugQrCode.classList.add("active");

			//делаем не активной иконку Whatsapp
			this.iconWhatsapp.classList.remove("active");

			//открываем кнопку
			this.loaderButton.setEnable();

			//если вебсокет закрыт, не продолжаем
			if (this.webSocket.readyState === WebSocket.CLOSED) return;

			//закрываем websocket
			this.webSocket.close();
		}
	}
}
