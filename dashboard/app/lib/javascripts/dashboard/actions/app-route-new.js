//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.NewAppRoute = {
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
