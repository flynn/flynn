//= require ../store
//= require ../dispatcher

(function () {
"use strict";

var App = FlynnDashboard.Stores.App = FlynnDashboard.Store.createClass({
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
			serviceUnavailable: false
		};
	},

	didBecomeActive: function () {
		this.__fetchApp();
		this.__fetchAppRelease().then(this.__fetchAppFormation.bind(this));
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
		return FlynnDashboard.client.getApp(this.props.appId).then(function (args) {
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

		return FlynnDashboard.client.getAppRelease(this.props.appId).then(function (args) {
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

		return FlynnDashboard.client.getAppFormation(this.props.appId, this.state.release.id).then(function (args) {
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
		var client = FlynnDashboard.client;
		var appId = this.props.appId;
		return this.__releaseLock.then(function () {
			return client.createRelease(release).then(function (args) {
				var res = args[0];
				var releaseId = res.id;
				return client.createAppRelease(appId, {id: releaseId});
			}.bind(this)).then(function () {
				FlynnDashboard.Dispatcher.handleStoreEvent({
					name: "APP:RELEASE_CREATED",
					appId: appId
				});
			}.bind(this));
		}.bind(this));
	},

	__createAppFormation: function (formation) {
		return this.__formationLock.then(function () {
			return FlynnDashboard.client.createAppFormation(formation.app, formation).then(function (args) {
				var res = args[0];
				this.setState({
					formation: res
				});
			}.bind(this));
		}.bind(this));
	},

	__deleteApp: function () {
		var appId = this.props.appId;
		return FlynnDashboard.client.deleteApp(appId).then(function (args) {
			FlynnDashboard.Dispatcher.handleStoreEvent({
				name: "APP:DELETED",
				appId: appId
			});
			return args;
		});
	},

	__deployCommit: function (ownerLogin, repoName, branchName, sha) {
		var client = FlynnDashboard.client;
		var appId = this.props.appId;
		var app, meta, release, artifactId;

		function createRelease () {
			return client.createRelease(Marbles.Utils.extend({}, release, {
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
				FlynnDashboard.Dispatcher.handleStoreEvent({
					name: "APP:JOB_CREATED",
					appId: appId,
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
				FlynnDashboard.Dispatcher.handleStoreEvent({
					name: "APP:DEPLOY_FAILED",
					appId: appId,
					errorMsg: "Something went wrong"
				});
				throw args;
			} else {
				var res = args[0];
				var xhr = args[1];
				FlynnDashboard.Dispatcher.handleStoreEvent({
					name: "APP:DEPLOY_FAILED",
					appId: appId,
					errorMsg: res.message || "Something went wrong ["+ xhr.status +"]"
				});
			}
		}).then(function () {
			FlynnDashboard.Dispatcher.handleStoreEvent({
				name: "APP:DEPLOY_SUCCESS",
				appId: appId
			});
		});
	}
});

App.dispatcherIndex = App.registerWithDispatcher(FlynnDashboard.Dispatcher);

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
		return FlynnDashboard.client.getApp(appId).then(function (args) {
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
	return this.createFromGithub(meta, appData);
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
	return this.createFromGithub(meta, appData);
};

App.createFromGithub = function (meta, appData) {
	var data = {
		name: appData.name,
		meta: meta
	};

	var appId, appName, databaseEnv, artifactId;

	var client = FlynnDashboard.client;

	function createDatabase () {
		return client.createAppDatabase({ apps: [appId] }).then(function (args) {
			var res = args[0];
			databaseEnv = res.env;
			FlynnDashboard.Dispatcher.handleStoreEvent({
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
			FlynnDashboard.Dispatcher.handleStoreEvent({
				name: "APP:JOB_CREATED",
				appId: appId,
				appName: appName,
				job: args[0]
			});
			return args;
		});
	}

	return client.createApp(data).then(function (args) {
		var res = args[0];
		appId = res.id;
		appName = res.name;
		FlynnDashboard.Dispatcher.handleStoreEvent({
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
			FlynnDashboard.Dispatcher.handleStoreEvent({
				name: "APP:CREATE_FAILED",
				appName: data.name,
				errorMsg: "Something went wrong"
			});
			throw args;
		} else {
			var res = args[0];
			var xhr = args[1];
			FlynnDashboard.Dispatcher.handleStoreEvent({
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
FlynnDashboard.Dispatcher.register(App.handleEvent);

})();
