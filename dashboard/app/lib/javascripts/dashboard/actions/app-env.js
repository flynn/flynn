//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.AppEnv = {
	createRelease: function (storeId, release) {
		Dispatcher.handleViewAction({
			name: "APP_ENV:CREATE_RELEASE",
			storeId: storeId,
			release: release
		});
	}
};

})();
