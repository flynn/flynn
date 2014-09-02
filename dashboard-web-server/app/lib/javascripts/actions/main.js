//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = FlynnDashboard.Dispatcher;

FlynnDashboard.Actions.Main = {
	handleLoginBtnClick: function () {
		Dispatcher.handleViewAction({
			name: "LOGOUT_BTN_CLICK"
		});
	}
};

})();
