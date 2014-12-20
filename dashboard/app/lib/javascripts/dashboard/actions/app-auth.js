//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.AppAuth = {
	setGithubToken: function (token) {
		Dashboard.config.setGithubToken(token);
	},

	createRelease: function (storeId, release) {
		Dispatcher.handleViewAction({
			name: "APP_ENV:CREATE_RELEASE",
			storeId: storeId,
			release: release
		});
	}
};

})();
