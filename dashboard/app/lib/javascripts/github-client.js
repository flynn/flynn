//= require ./dispatcher
(function () {
"use strict";

FlynnDashboard.GithubClient = Marbles.Utils.createClass({
	displayName: "GithubClient",

	mixins: [{
		ctor: {
			middleware: [
				Marbles.HTTP.Middleware.SerializeJSON
			]
		}
	}],

	willInitialize: function (accessToken) {
		if ( !accessToken ) {
			throw new Error(this.constructor.displayName +": Invalid client: "+ JSON.stringify(accessToken));
		}
		this.accessToken = accessToken;
	},

	performRequest: function (method, path, args) {
		args = args || {};

		if ( !path ) {
				var err = new Error(this.constructor.displayName +".prototype.performRequest(): Can't make request without path");
			setTimeout(function () {
				throw err;
			}.bind(this), 0);
			return Promise.reject(err);
		}

		var middleware = args.middleware || [];
		delete args.middleware;

		middleware = middleware.concat([{
			willSendRequest: function (request) {
				request.setRequestHeader("Authorization", "token "+ this.accessToken);
			}.bind(this)
		}]);

		return Marbles.HTTP(Marbles.Utils.extend({
			method: method,
			middleware: [].concat(this.constructor.middleware).concat(middleware),
			headers: Marbles.Utils.extend({
				Accept: 'application/json'
			}, args.headers || {}),
			url: "https://api.github.com" + path
		}, args)).then(function (args) {
			var res = args[0];
			var xhr = args[1];
			return new Promise(function (resolve, reject) {
				if (xhr.status >= 200 && xhr.status < 400) {
					resolve([res, xhr]);
				} else {
					if (xhr.status === 401) {
						FlynnDashboard.Dispatcher.handleAppEvent({
							name: "GITHUB_AUTH_CHANGE",
							authenticated: false
						});
					}
					reject([res, xhr]);
				}
			});
		});
	},

	getUser: function () {
		return this.performRequest('GET', '/user');
	},

	getRepo: function (owner, repo) {
		return this.performRequest('GET', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo));
	},

	getOrgs: function () {
		return this.performRequest('GET', '/user/orgs');
	},

	getRepos: function (params) {
		return this.performRequest('GET', '/user/repos', { params: params });
	},

	getStarredRepos: function (params) {
		return this.performRequest('GET', '/user/starred', { params: params });
	},

	getOrgRepos: function (params) {
		params = [].concat(params);
		params[0] = Marbles.Utils.extend({}, params[0]);
		var org = params[0].org;
		delete params[0].org;
		return this.performRequest('GET', '/orgs/'+ encodeURIComponent(org) +'/repos', {params: params});
	},

	getPulls: function (owner, repo, params) {
		return this.performRequest('GET', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo) +'/pulls', {params:params});
	},

	getPull: function (owner, repo, number) {
		return this.performRequest('GET', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo) +'/pulls/'+ encodeURIComponent(number));
	},

	mergePull: function (owner, repo, number, message) {
		return this.performRequest('PUT', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo) +'/pulls/'+ encodeURIComponent(number) +'/merge', {
			headers: {
				'Content-Type': 'application/json'
			},
			body: {
				commit_message: message
			}
		});
	},

	getBranches: function (owner, repo, params) {
		return this.performRequest('GET', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo) +'/branches', {params:params});
	},

	getCommits: function (owner, repo, params) {
		return this.performRequest('GET', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo) +'/commits', {params:params});
	},

	getCommit: function (owner, repo, sha) {
		return this.performRequest('GET', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo) +'/commits/'+ encodeURIComponent(sha));
	}
});

})();
