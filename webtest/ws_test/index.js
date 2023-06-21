(() => {

	//по загрузке страницы
	window.addEventListener("load", () => {

		//получаем изображение QR кода
		const imageQrCode = document.querySelector(".qr_code_image > img");

		//получаем заглушку QR кода
		const plugQrCode = document.querySelector(".plug_qr_code");

		//получаем иконку Whatsapp
		const iconWhatsapp = document.querySelector(".icon_whatsapp");

		//получаем кнопку сканировать QR код
		const btnScanQrCode = document.querySelector(".btn.scan_qr");

		//привязываем метод на клик по кнопке сканировать QR код
		btnScanQrCode.addEventListener("click", () => {

			//устанавливаем сокет соединение
			const webSocket = new WebSocket(`ws://127.0.0.1:10001/ws`);

			let keepAliveTimer;

			//открытие соединения
			webSocket.onopen = () => {

				//выводим лог
				console.log("Connection established");

				//запускаем таймер keep alive
				keepAliveTimer = setInterval(() => {

					//отправляем ping
					webSocket.send("__ping__");

				}, 30000);
			};

			//закрытие соединения
			webSocket.onclose = event => {

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
				//toggleContols(false);

				//сбрасываем таймер keep alive
				clearInterval(keepAliveTimer);
			};

			//ошибка
			webSocket.onerror = error => {

				//выводим ошибку на консоль
				console.log(`Error: ${error.message}`);

				//убираем панель QR кода
				//toggleContols(false);
			};

			//получение сообщений
			webSocket.onmessage = event => {

				try {

					//проверяем полученные данные
					if (!event.data || event.data === "__pong__") return;

					//десериализуем ответ из JSON
					const dataMessage = JSON.parse(event.data);

					//смотрим тип сообщения
					switch (dataMessage.type) {

						case "error": //ошибка
						{
							console.log(dataMessage.reason)
						}
							return;
						case "account": //данные авторизованного инстанса Whatsapp
						{
							//создаем модальную форму
							const modalForm = new ModalForm("Account data", "row");

							//фраза для модального окна
							let phrase;

							if (dataMessage.pushname && dataMessage.pushname !== "UNDEFINED" && dataMessage.wid) {

								phrase = `<h5 class="white">[${dataMessage.pushname}]:[${dataMessage.wid}] successfully authorized</h5>`;

							} else if (dataMessage.wid) {

								phrase = `<h5 class="white">[${dataMessage.wid}] successfully authorized</h5>`;

							} else {

								phrase = `<h5 class="white">Account successfully authorized</h5>`;
							}

							//открываем модальную форму
							//modalForm.showModalWithHtml(phrase);

							//убираем панель QR кода
							//toggleContols(false);
						}
							return;
						case "qr": //изображение QR кода
						{
							//выводим изображение QR кода
							imageQrCode.src = dataMessage.imageQrCode;

							//открываем панель QR кода
							toggleContols(true);
						}
							return;
					}

				} catch (exception) {

					//выводим ошибку на консоль
					console.error("Error: ", exception);
				}
			};
		});
	});

})();
