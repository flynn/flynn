import { createClass, extend } from 'marbles/utils';
import HTTP from 'marbles/http';
import SerializeJSONMiddleware from 'marbles/http/middleware/serialize_json';
import Dispatcher from './dispatcher';

var GithubClient = createClass({
	displayName: "GithubClient",

	mixins: [{
		ctor: {
			middleware: [
				SerializeJSONMiddleware
			]
		}
	}],

	willInitialize: function (accessToken, apiURL) {
		if ( !accessToken ) {
			throw new Error(this.constructor.displayName +": Invalid client: "+ JSON.stringify(accessToken));
		}
		this.accessToken = accessToken;
		this.apiURL = apiURL;
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

		return HTTP(extend({
			method: method,
			middleware: [].concat(this.constructor.middleware).concat(middleware),
			headers: extend({
				Accept: 'application/json'
			}, args.headers || {}),
			url: this.apiURL + path
		}, args)).then(function (args) {
			var res = args[0];
			var xhr = args[1];
			return new Promise(function (resolve, reject) {
				if (xhr.status >= 200 && xhr.status < 400) {
					resolve([res, xhr]);
				} else {
					if (xhr.status === 401) {
						Dispatcher.handleAppEvent({
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

	getRepoTree: function (owner, repo, ref, params) {
		params = params || [{}];
		return this.performRequest('GET', '/repos/'+ encodeURIComponent(owner) +'/'+ encodeURIComponent(repo) +'/git/trees/'+ encodeURIComponent(ref), {
			params: params
		});
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
		params[0] = extend({}, params[0]);
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

export default GithubClient;
