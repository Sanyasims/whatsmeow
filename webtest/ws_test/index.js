(() => {

	//по загрузке страницы
	window.addEventListener("load", () => {

		//создаем экземпляр класса авторизации
		const instanceAuthorization = new InstanceAuthorization();

		//запускаем авторизацию
		instanceAuthorization.startInstanceAuthorization();
	});

})();
