import { extend } from 'marbles/utils';
import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';

function createTaffyJob (client, taffyReleaseId, appID, appName, meta, appData) {
	var cloneURL = meta.clone_url;
	var ref = meta.ref;
	var sha = meta.sha;
	return client.createTaffyJob({
		release: taffyReleaseId,
		release_env: true,
		cmd: [appName, cloneURL, ref, sha],
		meta: extend({}, meta, {
			app: appID,
		})
	}).then(function (args) {
		Dispatcher.handleStoreEvent({
			name: "APP:JOB_CREATED",
			appId: appID,
			appName: appName,
			appData: appData || null,
			job: args[0]
		});
		return args;
	});
}

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
			release: null,
			formation: null,
			serviceUnavailable: false,
			notFound: false
		};
	},

	didBecomeActive: function () {
		this.__fetchApp();
		Dispatcher.dispatch({
			name: 'GET_APP_RELEASE',
			appID: this.props.appId,
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
							release: event.data.id
						}),
						app: extend({}, this.state.app, {
							release_id: event.data.id
						}),
						release: event.data
					});
					if ((this.state.formation || {}).release !== event.data.id) {
						Dispatcher.dispatch({
							name: 'GET_APP_FORMATION',
							appID: this.props.appId,
							releaseID: event.data.id
						});
					}
				}
			break;

			case 'APP_FORMATION':
				if (event.app === this.props.appId && event.data.release === this.state.release.id) {
					this.setState({
						formation: event.data
					});
				}
			break;

			case 'SCALE':
				var releaseID = event.object_id.split(':')[1];
				if (event.app === this.props.appId && event.data !== null) {
					this.setState({
						formation: extend({}, this.state.formation, {
							release: releaseID,
							processes: event.data || {}
						})
					});
				}
			break;

			case 'DEPLOYMENT':
				if ((this.release || {}).id === event.data.release && event.data.status === 'failed') {
					Dispatcher.dispatch({
						name: 'GET_APP_RELEASE',
						appID: this.props.appId,
					});
				}
			break;

			case "APP_PROCESSES:CREATE_FORMATION":
				this.__createAppFormation(event.formation);
			break;

			case "APP_DELETE:DELETE_APP":
				this.__deleteApp();
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
	},

	__deleteApp: function () {
		var __appId = this.id.appId;
		return App.getClient.call(this).deleteApp(this.props.appId).then(function (args) {
			Dispatcher.handleStoreEvent({
				name: "APP:DELETED",
				appId: __appId
			});
			return args;
		});
	}
});

App.getClient = function () {
	return Config.client;
};

App.isValidId = function (id) {
	return !!id.appId;
};

App.dispatcherIndex = App.registerWithDispatcher(Dispatcher);

App.findOrFetch = function (appId) {
	var instances = this.__instances;
	var instance;
	var app;
	for (var k in instances) {
		if (instances.hasOwnProperty(k)) {
			instance = instances[k];
			if (instance.id.appId === appId) {
				app = instance.state.app;
				break;
			}
		}
	}

	if (app) {
		return Promise.resolve(app);
	} else {
		return App.getClient.call(this).getApp(appId).then(function (args) {
			return args[0];
		});
	}
};

App.createFromGithubCommit = function (repo, branchName, sha, appData) {
	var meta = {
		type: "github",
		user_login: repo.ownerLogin,
		repo_name: repo.name,
		ref: branchName,
		sha: sha,
		clone_url: repo.cloneURL
	};
	var client = App.getClient.call(this);
	return this.createFromGithub(client, meta, appData);
};

App.createFromGithubPull = function (repo, pull, appData) {
	var meta = {
		type: "github",
		user_login: repo.ownerLogin,
		repo_name: repo.name,
		ref: pull.head.ref,
		sha: pull.head.sha,
		clone_url: repo.cloneURL,
		pull_number: String(pull.number),
		pull_user_login: pull.user.login,
		base_user_login: pull.base.ownerLogin,
		base_repo_name: pull.base.name,
		base_ref: pull.base.ref,
		base_sha: pull.base.sha
	};
	var client = App.getClient.call(this);
	return this.createFromGithub(client, meta, appData);
};

App.createFromGithub = function (client, meta, appData) {
	var data = {
		name: appData.name,
		meta: meta
	};

	var appId, appName;
	var databaseEnv = {};

	function createDatabase () {
		return client.createAppDatabase({ apps: [appId] }).then(function (args) {
			var res = args[0];
			databaseEnv = res.env;
			Dispatcher.handleStoreEvent({
				name: "APP:DATABASE_CREATED",
				appId: appId,
				appName: appName,
				env: extend({}, appData.env, databaseEnv)
			});
			return createRelease();
		});
	}

	function createRelease () {
		return client.createRelease({
			env: extend({}, appData.env, databaseEnv)
		}).then(function (args) {
			var res = args[0];
			return createAppRelease(res.id);
		});
	}

	function createAppRelease (releaseId) {
		return client.createAppRelease(appId, {
			id: releaseId
		}).then(function () {
			return getTaffyRelease();
		});
	}

	function getTaffyRelease () {
		return client.getTaffyRelease().then(function (args) {
			var res = args[0];
			return createTaffyJob(client, res.id, appId, appName, meta, appData);
		});
	}

	return client.createApp(data).then(function (args) {
		var res = args[0];
		appId = res.id;
		appName = res.name;
		Dispatcher.handleStoreEvent({
			name: "APP:CREATED",
			app: res
		});
		if (appData.dbRequested) {
			return createDatabase();
		} else {
			return createRelease();
		}
	}).catch(function (args) {
		if (args instanceof Error) {
			Dispatcher.handleStoreEvent({
				name: "APP:CREATE_FAILED",
				appName: data.name,
				errorMsg: "Something went wrong"
			});
			throw args;
		} else {
			var res = args[0];
			var xhr = args[1];
			Dispatcher.handleStoreEvent({
				name: "APP:CREATE_FAILED",
				appName: data.name,
				errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
			});
		}
	});
};

App.handleEvent = function (event) {
	switch (event.name) {
		case "GITHUB_DEPLOY:LAUNCH_FROM_COMMIT":
			App.createFromGithubCommit(event.repo, event.branchName, event.commit.sha, event.appData);
		break;

		case "GITHUB_DEPLOY:LAUNCH_FROM_PULL":
			App.createFromGithubPull(event.repo, event.pull, event.appData);
		break;
	}
};
Dispatcher.register(App.handleEvent);

App.isSystemApp = function (app) {
	return app.meta && app.meta["flynn-system-app"] === "true";
};

export default App;
