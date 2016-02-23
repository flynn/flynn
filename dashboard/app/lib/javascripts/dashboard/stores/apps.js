import State from 'marbles/state';
import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';

var Apps = Store.createClass({
	displayName: "Stores.Apps",

	getState: function () {
		return this.state;
	},

	didInitialize: function () {},

	didBecomeActive: function () {
		this.__fetchApps();
	},

	getInitialState: function () {
		return {
			fetched: false,
			apps: []
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'APP':
			this.__addOrReplaceApp(event.data);
			break;

		case 'APP_DELETED':
			this.__handleAppDeleted(event.app);
			break;
		}
	},

	__fetchApps: function () {
		return this.__getClient().getApps().then(function (args) {
			var res = args[0];
			this.setState({
				fetched: true,
				apps: res
			});
		}.bind(this));
	},

	__addOrReplaceApp: function (app) {
		var apps = [];
		var appFound = false;
		this.state.apps.forEach(function (a) {
			if (a.id === app.id) {
				appFound = true;
				apps.push(app);
			} else {
				apps.push(a);
			}
		});
		if ( !appFound ) {
			this.__handleAppCreated(app);
		} else {
			this.setState({
				apps: apps
			});
		}
	},

	__handleAppCreated: function (app) {
		var apps = [app].concat(this.state.apps);
		this.setState({
			apps: apps
		});
	},

	__handleAppDeleted: function (appId) {
		var apps = this.state.apps.filter(function (app) {
			return app.id !== appId;
		});
		this.setState({
			apps: apps
		});
	},

	__getClient: function () {
		return Config.client;
	}

}, State);

Apps.registerWithDispatcher(Dispatcher);

export default Apps;
