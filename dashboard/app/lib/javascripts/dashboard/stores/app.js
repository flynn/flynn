import { extend } from 'marbles/utils';
import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';

var App = Store.createClass({
	displayName: "Stores.App",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = this.id;

		this.__formationLock = Promise.resolve();
		this.__releaseLock = Promise.resolve();
	},

	getInitialState: function () {
		return {
			app: null,
			release: {
				meta: {},
				env: {},
				processes: {}
			},
			formation: null,
			serviceUnavailable: false,
			notFound: false,
			deleteError: null
		};
	},

	didBecomeActive: function () {
		this.__fetchApp();
		Dispatcher.dispatch({
			name: 'GET_APP_RELEASE',
			appID: this.props.appId
		});
	},

	didBecomeInactive: function () {
		this.constructor.discardInstance(this);
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'APP':
			if (event.app === this.props.appId) {
				this.setState({
					app: event.data
				});
			}
			break;

		case 'APP_RELEASE':
			if (event.app === this.props.appId) {
				this.setState({
					formation: this.state.formation === null ? null : extend({}, this.state.formation, {
						release: event.object_id
					}),
					app: extend({}, this.state.app, {
						release: event.object_id
					}),
					release: event.data.release
				});
				if ((this.state.formation || {}).release !== event.object_id) {
					Dispatcher.dispatch({
						name: 'GET_APP_FORMATION',
						appID: this.props.appId,
						releaseID: event.object_id
					});
				}
			}
			break;

		case 'APP_FORMATION':
		case 'APP_FORMATION_NOT_FOUND':
			if (event.app === this.props.appId && event.data.release === this.state.release.id) {
				this.setState({
					formation: extend({}, event.data, {
						processes: (function () {
							var processes = {};
							var formationProcesses = event.data.processes;
							Object.keys(this.state.release.processes || {}).forEach(function (k) {
								processes[k] = formationProcesses[k] || 0;
							});
							return processes;
						}.bind(this))()
					})
				});
			}
			break;

		case 'SCALE':
			if (event.app === this.props.appId && event.data.processes !== null) {
				this.setState({
					formation: extend({}, this.state.formation, {
						release: event.data.release,
						processes: event.data.processes || {}
					})
				});
			}
			break;

		case 'DEPLOYMENT':
			if ((this.release || {}).id === event.data.release && event.data.status === 'failed') {
				Dispatcher.dispatch({
					name: 'GET_APP_RELEASE',
					appID: this.props.appId
				});
			}
			break;

		case "APP_PROCESSES:CREATE_FORMATION":
			this.__createAppFormation(event.formation);
			break;

		case "DELETE_APP_FAILED":
			this.setState({
				deleteError: event.error ? event.error : ('Something went wrong ('+ event.status +')')
			});
			break;
		}
	},

	__withoutChangeEvents: function (fn) {
		var handleChange = this.handleChange;
		this.handleChange = function(){};
		return fn().then(function () {
			this.handleChange = handleChange;
		}.bind(this));
	},

	__getApp: function () {
		if (this.state.app) {
			return Promise.resolve(this.state.app);
		} else {
			return this.__fetchApp();
		}
	},

	__fetchApp: function () {
		return App.getClient.call(this).getApp(this.props.appId).then(function (args) {
			var res = args[0];
			if (res.name === 'dashboard') {
				Config.setDashboardAppID(res.id);
			}
			this.setState({
				app: res
			});
		}.bind(this)).catch(function (args) {
			if (args instanceof Error) {
				return Promise.reject(args);
			} else {
				var xhr = args[1];
				if (xhr.status === 503) {
					this.setState({
						serviceUnavailable: true
					});
				} else if (xhr.status === 404) {
					this.setState({
						notFound: true
					});
				} else {
					return Promise.reject(args);
				}
			}
		}.bind(this));
	},

	__createAppFormation: function (formation) {
		return this.__formationLock.then(function () {
			return App.getClient.call(this).createAppFormation(formation.app, formation).then(function (args) {
				var res = args[0];
				this.setState({
					formation: res
				});
			}.bind(this));
		}.bind(this));
	}
});

App.getClient = function () {
	return Config.client;
};

App.isValidId = function (id) {
	return !!id.appId;
};

App.dispatcherIndex = App.registerWithDispatcher(Dispatcher);

App.isSystemApp = function (app) {
	return app.meta && app.meta["flynn-system-app"] === "true";
};

export default App;
