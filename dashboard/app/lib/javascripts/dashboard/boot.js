//= require ./routers/apps
//= require ./routers/github

(function () {

"use strict";

Dashboard.dispatcherIndex = Dashboard.Dispatcher.register(
	Dashboard.__handleEvent.bind(Dashboard));

var appsRouter = new Dashboard.routers.Apps();
appsRouter.dispatcherIndex = Dashboard.Dispatcher.register(
	appsRouter.handleEvent.bind(appsRouter)
);

var githubRouter = new Dashboard.routers.Github();
githubRouter.dispatcherIndex = Dashboard.Dispatcher.register(
	githubRouter.handleEvent.bind(githubRouter)
);

Dashboard.config.fetch().catch(
		function(){}); // suppress SERVICE_UNAVAILABLE error

})();
