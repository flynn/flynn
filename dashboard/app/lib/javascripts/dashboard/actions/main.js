//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.Main = {
	handleLoginBtnClick: function () {
		Dispatcher.handleViewAction({
			name: "LOGOUT_BTN_CLICK"
		});
	}
};

})();
