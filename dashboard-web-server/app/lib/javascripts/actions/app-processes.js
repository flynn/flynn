//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = FlynnDashboard.Dispatcher;

FlynnDashboard.Actions.AppProcesses = {
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
