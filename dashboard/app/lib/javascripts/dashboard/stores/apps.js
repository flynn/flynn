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
			apps: [],
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
			case "APP:CREATED":
				this.__handleAppCreated(event.app);
			break;

			case "APP:DELETED":
				this.__handleAppDeleted(event.appId);
			break;
		}
	},

	__fetchApps: function () {
		return this.__getClient().getApps().then(function (args) {
			var res = args[0];
			this.setState({
				apps: res,
			});
		}.bind(this));
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
