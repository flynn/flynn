//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.AppDelete = {
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
