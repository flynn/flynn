(function () {

"use strict";

Dashboard.Client = Marbles.Utils.createClass({
	displayName: "Client",

	mixins: [{
		ctor: {
			middleware: [
				Marbles.HTTP.Middleware.SerializeJSON,
				Marbles.HTTP.Middleware.WithCredentials
			]
		}
	}],

	willInitialize: function (endpoints) {
		this.endpoints = endpoints;
		this.__cachedResponses = {};
	},

	performRequest: function (method, args) {
		if ( !args.url ) {
				var err = new Error(this.constructor.displayName +".prototype.performRequest(): Can't make request without URL");
			setTimeout(function () {
				throw err;
			}.bind(this), 0);
			return Promise.reject(err);
		}

		var cacheResponse = args.cacheResponse;
		var cachedResponseKey = method + args.url;
		var cachedRepsonses = this.__cachedResponses;
		if (cacheResponse && cachedRepsonses[cachedResponseKey]) {
			return Promise.resolve(cachedRepsonses[cachedResponseKey]);
		}

		var middleware = args.middleware || [];
		delete args.middleware;

		return Marbles.HTTP(Marbles.Utils.extend({
			method: method,
			middleware: [].concat(this.constructor.middleware).concat(middleware),
		}, args)).then(function (args) {
			var res = args[0];
			var xhr = args[1];
			return new Promise(function (resolve, reject) {
				if (xhr.status >= 200 && xhr.status < 400) {
					if (cacheResponse) {
						cachedRepsonses[cachedResponseKey] = [res, xhr];
					}
					resolve([res, xhr]);
				} else {
					if (xhr.status === 401) {
						Dashboard.config.fetch();
					}

					reject([res, xhr]);
				}
			});
		});
	},

	performControllerRequest: function (method, args) {
		var controllerKey = (Dashboard.config.user || {}).controller_key;
		return this.performRequest(method, Marbles.Utils.extend({}, args, {
			middleware: [
				Marbles.HTTP.Middleware.BasicAuth("", controllerKey)
			],
			url: this.endpoints.cluster_controller + args.url
		}));
	},

	login: function (token) {
		return this.performRequest('POST', {
			url: this.endpoints.login,
			body: {
				token: token
			},
			headers: {
				'Content-Type': 'application/json'
			}
		}).then(function (args) {
			return Dashboard.config.fetch().then(function () {
				return args;
			});
		});
	},

	logout: function () {
		return this.performRequest('DELETE', {
			url: this.endpoints.logout
		}).then(function (args) {
			return Dashboard.config.fetch().then(function () {
				return args;
			});
		});
	},

	getApps: function () {
		return this.performControllerRequest('GET', {
			url: "/apps"
		});
	},

	getApp: function (appId) {
		return this.performControllerRequest('GET', {
			url: "/apps/"+ encodeURIComponent(appId)
		});
	},

	getAppRelease: function (appId) {
		return this.performControllerRequest('GET', {
			url: "/apps/"+ encodeURIComponent(appId) +"/release"
		});
	},

	getAppFormation: function (appId, releaseId) {
		return this.performControllerRequest('GET', {
			url: "/apps/"+ encodeURIComponent(appId) +"/formations/"+ encodeURIComponent(releaseId)
		});
	},

	getAppJobs: function (appId) {
		return this.performControllerRequest('GET', {
			url: "/apps/"+ encodeURIComponent(appId) +"/jobs"
		});
	},

	getAppRoutes: function (appId) {
		return this.performControllerRequest('GET', {
			url: "/apps/"+ encodeURIComponent(appId) +"/routes"
		});
	},

	getAppResources: function (appId) {
		return this.performControllerRequest('GET', {
			url: "/apps/"+ encodeURIComponent(appId) +"/resources"
		});
	},

	createAppRoute: function (appId, data) {
		return this.performControllerRequest('POST', {
			url: "/apps/"+ encodeURIComponent(appId) +"/routes",
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	deleteAppRoute: function (appId, routeType, routeId) {
		return this.performControllerRequest('DELETE', {
			url: "/apps/"+ appId +"/routes/"+ routeType +"/"+ routeId
		});
	},

	createApp: function (data) {
		return this.performControllerRequest('POST', {
			url: "/apps",
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	updateApp: function (appId, data) {
		return this.performControllerRequest('POST', {
			url: "/apps/"+ encodeURIComponent(appId),
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	deleteApp: function (appId) {
		return this.performControllerRequest('DELETE', {
			url: "/apps/"+ encodeURIComponent(appId)
		});
	},

	createAppDatabase: function (data) {
		return this.performControllerRequest('POST', {
			url: "/providers/postgres/resources",
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	createArtifact: function (data) {
		return this.performControllerRequest('POST', {
			url: "/artifacts",
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	createRelease: function (data) {
		return this.performControllerRequest('POST', {
			url: "/releases",
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	deployAppRelease: function (appId, releaseId) {
		return this.performControllerRequest('POST', {
			url: "/apps/"+ appId +"/deploy",
			body: {id: releaseId},
			headers: {
				'Content-Type': 'application/json'
			}
		}).then(function (args) {
			var res = args[0];
			if (res.finished_at) {
				return args;
			}
			return this.waitForDeployment(res.id).then(function () {
				return args;
			}, function (err) {
				return Promise.reject([err]);
			});
		}.bind(this));
	},

	waitForDeployment: function (deploymentId) {
		if ( !window.hasOwnProperty('EventSource') ) {
			return Promise.reject('window.EventSource not defined');
		}

		var controllerKey = (Dashboard.config.user || {}).controller_key;
		var url = this.endpoints.cluster_controller + "/deployments/"+ encodeURIComponent(deploymentId);
		url = url + "?key="+ encodeURIComponent(controllerKey);
		return new Promise(function (resolve, reject) {
			var es = new window.EventSource(url, {withCredentials: true});

			setTimeout(function () {
				reject("Timed out waiting for deployment completion");
				es.close();
			}, 10000);

			es.addEventListener("error", function (e) {
				reject(e);
				es.close();
			});
			es.addEventListener("complete", function () {
				resolve();
				es.close();
			});
			es.addEventListener("message", function (e) {
				var data = JSON.parse(e.data);
				if (data.status === "complete") {
					resolve();
					es.close();
				}
			});
		}.bind(this));
	},

	createAppRelease: function (appId, data) {
		return this.performControllerRequest('PUT', {
			url: "/apps/"+ appId +"/release",
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	createAppFormation: function (appId, data) {
		return this.performControllerRequest('PUT', {
			url: "/apps/"+ appId +"/formations/"+ data.release,
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	getTaffyRelease: function () {
		return this.performControllerRequest('GET', {
			cacheResponse: true, // response doesn't change
			url: "/apps/taffy/release"
		});
	},

	createTaffyJob: function (data) {
		return this.performControllerRequest('POST', {
			url: "/apps/taffy/jobs",
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},
});

})();
