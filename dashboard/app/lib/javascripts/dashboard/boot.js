//= require ./routers/apps
//= require ./routers/github

(function () {

"use strict";

Dashboard.dispatcherIndex = Dashboard.Dispatcher.register(
	Dashboard.__handleEvent.bind(Dashboard));

var appsRouter = new this.routers.Apps();
appsRouter.dispatcherIndex = this.Dispatcher.register(
	appsRouter.handleEvent.bind(appsRouter)
);

var githubRouter = new this.routers.Github();
githubRouter.dispatcherIndex = Dashboard.Dispatcher.register(
	githubRouter.handleEvent.bind(githubRouter)
);

Dashboard.config.fetch().catch(
		function(){}); // suppress SERVICE_UNAVAILABLE error

})();
