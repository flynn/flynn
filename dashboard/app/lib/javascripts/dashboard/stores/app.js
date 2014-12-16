//= require ../store
//= require ../dispatcher
//= require ../client

(function () {
"use strict";

var App = Dashboard.Stores.App = Dashboard.Store.createClass({
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
		this.__fetchAppRelease().then(this.__fetchAppFormation.bind(this)).catch(function (args) {
			// ignore 404 errors
			if (args && args[1] && args[1].status === 404) {
				return;
			}
			return Promise.reject(args);
		});
	},

	handleEvent: function (event) {
		switch (event.name) {
			case "APP_ENV:CREATE_RELEASE":
				this.__createRelease(event.release).then(function () {
					return this.__fetchAppRelease().then(this.__fetchAppFormation.bind(this));
				}.bind(this));
			break;

			case "APP_PROCESSES:CREATE_FORMATION":
				this.__createAppFormation(event.formation);
			break;

			case "APP_DELETE:DELETE_APP":
				this.__deleteApp();
			break;

			case "APP_DEPLOY_COMMIT:DEPLOY_COMMIT":
				this.__deployCommit(event.ownerLogin, event.repoName, event.branchName, event.sha);
			break;
		}
	},

	__getApp: function () {
		if (this.state.app) {
			return Promise.resolve(this.state.app);
		} else {
			return this.__fetchApp();
		}
	},

	__getAppRelease: function () {
		return this.__releaseLock.then(function () {
			if (this.state.release) {
				return Promise.resolve(this.state.release);
			} else {
				return this.__fetchAppRelease();
			}
		}.bind(this));
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

	__fetchAppRelease: function () {
		var releaseLockResolve;
		this.__releaseLock = new Promise(function (resolve) {
			releaseLockResolve = function (isError, args) {
				resolve();
				if (isError) {
					return Promise.reject(args);
				} else {
					return Promise.resolve(args);
				}
			}.bind(this);
		}.bind(this));

		return App.getClient.call(this).getAppRelease(this.props.appId).then(function (args) {
			var res = args[0];
			this.setState({
				release: res
			});
			return res;
		}.bind(this)).then(releaseLockResolve.bind(null, false), releaseLockResolve.bind(null, true));
	},

	__fetchAppFormation: function () {
		var formationLockResolve;
		this.__formationLock = new Promise(function (resolve) {
			formationLockResolve = function (isError, args) {
				resolve();
				if (isError) {
					return Promise.reject(args);
				} else {
					return Promise.resolve(args);
				}
			}.bind(this);
		}.bind(this));

		function buildProcesses (release) {
			var processes = {};
			Object.keys(release.processes).sort().forEach(function (k) {
				processes[k] = 0;
			});
			return processes;
		}

		return App.getClient.call(this).getAppFormation(this.props.appId, this.state.release.id).then(function (args) {
			var res = args[0];
			if ( !res.processes ) {
				res.processes = buildProcesses(this.state.release);
			}
			this.setState({
				formation: res
			});
			return res;
		}.bind(this), function (args) {
			if (args instanceof Error) {
				return Promise.reject(args);
			}
			var xhr = args[1];
			var release = this.state.release;
			var formation = {
				app: this.props.appId,
				release: release.id,
				processes: {}
			};
			if (xhr.status === 404) {
				formation.processes = buildProcesses(release);
				this.setState({
					formation: formation
				});
				return formation;
			} else {
				return Promise.reject(args);
			}
		}.bind(this)).then(formationLockResolve.bind(null, false), formationLockResolve.bind(null, true));
	},

	__createRelease: function (release) {
		var client = App.getClient.call(this);
		var __appId = this.id.appId;
		return this.__releaseLock.then(function () {
			return client.createRelease(release).then(function (args) {
				var res = args[0];
				var releaseId = res.id;
				return client.createAppRelease(this.props.appId, {id: releaseId});
			}.bind(this)).then(function () {
				Dashboard.Dispatcher.handleStoreEvent({
					name: "APP:RELEASE_CREATED",
					appId: __appId
				});
			}.bind(this)).catch(function (args) {
				var res = args[0];
				var xhr = args[1];
				Dashboard.Dispatcher.handleStoreEvent({
					name: "APP:RELEASE_CREATE_FAILED",
					appId: __appId,
					errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
				});
			}.bind(this));
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
			Dashboard.Dispatcher.handleStoreEvent({
				name: "APP:DELETED",
				appId: __appId
			});
			return args;
		});
	},

	__deployCommit: function (ownerLogin, repoName, branchName, sha) {
		var client = App.getClient.call(this);
		var appId = this.props.appId;
		var __appId = this.id.appId;
		var app, meta, release, artifactId;

		function createRelease () {
			return client.createRelease(Marbles.Utils.extend({}, release, {
				id: null,
				artifact: artifactId
			})).then(function (args) {
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
				return createTaffyJob(res.id);
			});
		}

		function createTaffyJob (taffyReleaseId) {
			return client.createTaffyJob({
				release: taffyReleaseId,
				cmd: [app.name, meta.clone_url, meta.ref, meta.sha]
			}).then(function (args) {
				Dashboard.Dispatcher.handleStoreEvent({
					name: "APP:JOB_CREATED",
					appId: __appId,
					job: args[0]
				});
				return args;
			});
		}

		return Promise.all([this.__getApp(), this.__getAppRelease(), this.__formationLock]).then(function (__args) {
			app = __args[0];
			release = __args[1];
			meta = Marbles.Utils.extend({}, app.meta, {
				ref: branchName,
				sha: sha
			});

			return client.createArtifact({
				type: "docker",
				uri: "example://uri"
			}).then(function (args) {
				var res = args[0];
				artifactId = res.id;
				return createRelease();
			}).then(function () {
				var data = {
					name: app.name,
					meta: meta
				};
				return client.updateApp(appId, data);
			});
		}).catch(function (args) {
			if (args instanceof Error) {
				Dashboard.Dispatcher.handleStoreEvent({
					name: "APP:DEPLOY_FAILED",
					appId: __appId,
					errorMsg: "Something went wrong"
				});
			} else {
				var res = args[0];
				var xhr = args[1];
				Dashboard.Dispatcher.handleStoreEvent({
					name: "APP:DEPLOY_FAILED",
					appId: __appId,
					errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
				});
			}
			return Promise.reject(args);
		}).then(function () {
			Dashboard.Dispatcher.handleStoreEvent({
				name: "APP:DEPLOY_SUCCESS",
				appId: __appId
			});
			this.setState({
				app: Marbles.Utils.extend({}, app, { meta: meta })
			});
		}.bind(this));
	}
});

App.getClient = function () {
	return Dashboard.client;
};

App.isValidId = function (id) {
	return !!id.appId;
};

App.dispatcherIndex = App.registerWithDispatcher(Dashboard.Dispatcher);

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

	var appId, appName, databaseEnv, artifactId;

	function createDatabase () {
		return client.createAppDatabase({ apps: [appId] }).then(function (args) {
			var res = args[0];
			databaseEnv = res.env;
			Dashboard.Dispatcher.handleStoreEvent({
				name: "APP:DATABASE_CREATED",
				appId: appId,
				appName: appName,
				env: Marbles.Utils.extend({}, appData.env, databaseEnv)
			});
			return createArtifact();
		});
	}

	function createArtifact () {
		return client.createArtifact({
			type: "docker",
			uri: "example://uri"
		}).then(function (args) {
			var res = args[0];
			artifactId = res.id;
			return createRelease();
		});
	}

	function createRelease () {
		return client.createRelease({
			env: Marbles.Utils.extend({}, appData.env, databaseEnv),
			artifact: artifactId
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
			return createTaffyJob(res.id);
		});
	}

	function createTaffyJob (taffyReleaseId) {
		return client.createTaffyJob({
			release: taffyReleaseId,
			cmd: [appName, meta.clone_url, meta.ref, meta.sha]
		}).then(function (args) {
			Dashboard.Dispatcher.handleStoreEvent({
				name: "APP:JOB_CREATED",
				appId: appId,
				appName: appName,
				appData: appData,
				job: args[0]
			});
			return args;
		});
	}

	return client.createApp(data).then(function (args) {
		var res = args[0];
		appId = res.id;
		appName = res.name;
		Dashboard.Dispatcher.handleStoreEvent({
			name: "APP:CREATED",
			app: res
		});
		if (appData.dbRequested) {
			return createDatabase();
		} else {
			return getTaffyRelease();
		}
	}).catch(function (args) {
		if (args instanceof Error) {
			Dashboard.Dispatcher.handleStoreEvent({
				name: "APP:CREATE_FAILED",
				appName: data.name,
				errorMsg: "Something went wrong"
			});
			throw args;
		} else {
			var res = args[0];
			var xhr = args[1];
			Dashboard.Dispatcher.handleStoreEvent({
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
Dashboard.Dispatcher.register(App.handleEvent);

})();
