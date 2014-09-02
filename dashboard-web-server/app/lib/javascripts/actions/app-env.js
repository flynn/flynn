//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = FlynnDashboard.Dispatcher;

FlynnDashboard.Actions.AppEnv = {
	createRelease: function (storeId, release) {
		Dispatcher.handleViewAction({
			name: "APP_ENV:CREATE_RELEASE",
			storeId: storeId,
			release: release
		});
	}
};

})();
