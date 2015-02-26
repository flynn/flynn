//= require ../store
//= require ./app

(function () {

"use strict";

var AppRoutes = Dashboard.Stores.AppRoutes = Dashboard.Store.createClass({
	displayName: "Stores.AppRoutes",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = {
			appId: this.id.appId
		};
	},

	didInitialize: function () {},

	didBecomeActive: function () {
		this.__fetchRoutes();
	},

	getInitialState: function () {
		return {
			routes: []
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
			case "NEW_APP_ROUTE:CREATE_ROUTE":
				Dashboard.Stores.App.findOrFetch(this.id.appId).then(function (app) {
					return this.__createAppRoute(event.domain, app.name);
				}.bind(this)).then(function () {
					return this.__fetchRoutes();
				}.bind(this));
			break;

			case "APP_ROUTE_DELETE:DELETE_ROUTE":
				this.__deleteAppRoute(event.routeType, event.routeId).then(function () {
					return this.__fetchRoutes();
				}.bind(this));
			break;
		}
	},

	__fetchRoutes: function () {
		return this.__getClient().getAppRoutes(this.props.appId).then(function (args) {
			var res = args[0];
			this.setState({
				routes: res
			});
		}.bind(this));
	},

	__createAppRoute: function (domain, appName) {
		var data = {
			type: "http",
			domain: domain,
			service: appName +"-web"
		};
		return this.__getClient().createAppRoute(this.props.appId, data).then(function () {
			Dashboard.Dispatcher.handleStoreEvent({
				name: "APP_ROUTES:CREATED",
				appId: this.id.appId
			});
		}.bind(this)).catch(function (args) {
			if (args instanceof Error) {
				throw args;
			} else {
				var res = args[0];
				var xhr = args[1];
				Dashboard.Dispatcher.handleStoreEvent({
					name: "APP_ROUTES:CREATE_FAILED",
					appId: this.props.appId,
					errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
				});
				return null;
			}
		}.bind(this));
	},

	__deleteAppRoute: function (routeType, routeId) {
		var routes = this.state.routes.filter(function (route) {
			return route.id !== routeId;
		});
		this.setState({
			routes: routes
		});

		return this.__getClient().deleteAppRoute(this.props.appId, routeType, routeId).then(function () {
			Dashboard.Dispatcher.handleStoreEvent({
				name: "APP_ROUTES:DELETED",
				appId: this.id.appId,
				routeId: routeId
			});
		}.bind(this)).catch(function (args) {
			if (args instanceof Error) {
				throw args;
			} else {
				var res = args[0];
				var xhr = args[1];
				Dashboard.Dispatcher.handleStoreEvent({
					name: "APP_ROUTES:DELETE_FAILED",
					appId: this.props.appId,
					routeId: routeId,
					errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
				});
				return null;
			}
		}.bind(this));
	},

	__getClient: function () {
		return Dashboard.client;
	}

}, Marbles.State);

AppRoutes.isValidId = function (id) {
	return !!id.appId;
};

AppRoutes.registerWithDispatcher(Dashboard.Dispatcher);

})();
