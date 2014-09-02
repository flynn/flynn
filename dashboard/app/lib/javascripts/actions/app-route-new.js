//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = FlynnDashboard.Dispatcher;

FlynnDashboard.Actions.NewAppRoute = {
	createAppRoute: function (appId, domain) {
		Dispatcher.handleViewAction({
			name: "NEW_APP_ROUTE:CREATE_ROUTE",
			storeId: {
				appId: appId
			},
			domain: domain
		});
	}
};

})();
