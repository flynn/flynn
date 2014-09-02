//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = FlynnDashboard.Dispatcher;

FlynnDashboard.Actions.AppRouteDelete = {
	deleteAppRoute: function (appId, routeId) {
		Dispatcher.handleViewAction({
			name: "APP_ROUTE_DELETE:DELETE_ROUTE",
			storeId: {
				appId: appId
			},
			routeId: routeId
		});
	}
};

})();
