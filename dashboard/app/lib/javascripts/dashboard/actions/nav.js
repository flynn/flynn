//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.Nav = {
	handleAuthBtnClick: function () {
		Dispatcher.handleViewAction({
			name: "AUTH_BTN_CLICK"
		});
	}
};

})();
