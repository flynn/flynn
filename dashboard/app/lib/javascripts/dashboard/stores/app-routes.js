import State from 'marbles/state';
import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';

var AppRoutes = Store.createClass({
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
			fetched: false,
			routes: []
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'ROUTE':
			if (event.app === this.props.appId) {
				this.__addOrReplaceRoute(event.data);
			}
			break;

		case 'ROUTE_DELETED':
			if (event.app === this.props.appId) {
				this.__removeRoute(event.object_id);
			}
			break;
		}
	},

	__addOrReplaceRoute: function (route) {
		var routes = [];
		var routeFound = false;
		this.state.routes.forEach(function (r) {
			if (r.id === route.id) {
				routeFound = true;
				routes.push(route);
			} else {
				routes.push(r);
			}
		});
		if ( !routeFound ) {
			routes.push(route);
		}
		this.setState({
			routes: routes
		});
	},

	__removeRoute: function (routeID) {
		var routes = this.state.routes.filter(function (r) {
			return r.id !== routeID;
		});
		this.setState({
			routes: routes
		});
	},

	__fetchRoutes: function () {
		return this.__getClient().getAppRoutes(this.props.appId).then(function (args) {
			var res = args[0];
			this.setState({
				fetched: true,
				routes: res
			});
		}.bind(this));
	},

	__getClient: function () {
		return Config.client;
	}

}, State);

AppRoutes.isValidId = function (id) {
	return !!id.appId;
};

AppRoutes.registerWithDispatcher(Dispatcher);

export default AppRoutes;
