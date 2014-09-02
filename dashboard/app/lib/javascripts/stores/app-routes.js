//= require ../store
//= require ./app

(function () {

"use strict";

var AppRoutes = FlynnDashboard.Stores.AppRoutes = FlynnDashboard.Store.createClass({
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
				FlynnDashboard.Stores.App.findOrFetch(this.props.appId).then(function (app) {
					return this.__createAppRoute(event.domain, app.name);
				}.bind(this)).then(function () {
					return this.__fetchRoutes();
				}.bind(this));
			break;

			case "APP_ROUTE_DELETE:DELETE_ROUTE":
				this.__deleteAppRoute(event.routeId).then(function () {
					return this.__fetchRoutes();
				}.bind(this));
			break;
		}
	},

	__fetchRoutes: function () {
		return FlynnDashboard.client.getAppRoutes(this.props.appId).then(function (args) {
			var res = args[0];
			this.setState({
				routes: res
			});
		}.bind(this));
	},

	__createAppRoute: function (domain, appName) {
		var data = {
			type: "http",
			config: {
				domain: domain,
				service: appName +"-web"
			}
		};
		return FlynnDashboard.client.createAppRoute(this.props.appId, data).then(function () {
			FlynnDashboard.Dispatcher.handleStoreEvent({
				name: "APP_ROUTES:CREATED",
				appId: this.props.appId
			});
		}.bind(this)).catch(function (args) {
			if (args instanceof Error) {
				throw args;
			} else {
				var res = args[0];
				var xhr = args[1];
				FlynnDashboard.Dispatcher.handleStoreEvent({
					name: "APP_ROUTES:CREATE_FAILED",
					appId: this.props.appId,
					errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
				});
				return null;
			}
		}.bind(this));
	},

	__deleteAppRoute: function (routeId) {
		var routes = this.state.routes.filter(function (route) {
			return route.id !== routeId;
		});
		this.setState({
			routes: routes
		});
		return FlynnDashboard.client.deleteAppRoute(this.props.appId, routeId).then(function () {
			FlynnDashboard.Dispatcher.handleStoreEvent({
				name: "APP_ROUTES:DELETED",
				appId: this.props.appId,
				routeId: routeId
			});
		}.bind(this)).catch(function (args) {
			if (args instanceof Error) {
				throw args;
			} else {
				var res = args[0];
				var xhr = args[1];
				FlynnDashboard.Dispatcher.handleStoreEvent({
					name: "APP_ROUTES:DELETE_FAILED",
					appId: this.props.appId,
					routeId: routeId,
					errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
				});
				return null;
			}
		}.bind(this));
	}

}, Marbles.State);

AppRoutes.registerWithDispatcher(FlynnDashboard.Dispatcher);

})();
