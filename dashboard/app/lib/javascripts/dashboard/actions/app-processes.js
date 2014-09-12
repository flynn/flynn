//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.AppProcesses = {
	createFormation: function (formation) {
		Dispatcher.handleViewAction({
			name: "APP_PROCESSES:CREATE_FORMATION",
			storeId: {
				appId: formation.app
			},
			formation: formation
		});
	}
};

})();
