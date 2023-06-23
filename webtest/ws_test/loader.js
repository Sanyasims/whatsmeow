/**
 * Класс вывода иконки загрузки в кнопках
 */
class Loader {

	/**
	 * Конструктор
	 * @param button - кнопка для вывода loader
	 */
	constructor(button) {

		//присваиваем кнопку
		this.button = button;

		//пишем тип loader
		this.type = "spinner";

		//ставим пустую строку в HTML кнопки до подстановки loader
		this.prevHtml = "";
	}

	/**
	 * Метод скрывает кнопку
	 */
	setDisable() {

		//закрываем кнопку
		this.button.setAttribute("disabled", "disabled");

		//получаем HTML из кнопки
		this.prevHtml = this.button.innerHTML;

		//смотрим тип loader
		switch (this.type) {

			case "spinner":

				//очищаем HTML в кнопке
				this.button.innerHTML = "";

				//создаем spinner
				const spinner = document.createElement("div");
				spinner.classList.add("spinner");
				spinner.innerHTML = `<div></div><div></div><div></div>`;

				//добавляем spinner в кнопку
				this.button.appendChild(spinner);

				break;
		}
	}

	/**
	 * Метод открывает кнопку
	 */
	setEnable() {

		//открываем кнопку
		this.button.removeAttribute("disabled");

		//возвращаем HTML кнопки до подстановки loader
		this.button.innerHTML = this.prevHtml;
	}
}
