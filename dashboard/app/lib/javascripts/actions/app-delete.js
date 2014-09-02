//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = FlynnDashboard.Dispatcher;

FlynnDashboard.Actions.AppDelete = {
	deleteApp: function (appId) {
		Dispatcher.handleViewAction({
			name: "APP_DELETE:DELETE_APP",
			storeId: {
				appId: appId
			}
		});
	}
};

})();
