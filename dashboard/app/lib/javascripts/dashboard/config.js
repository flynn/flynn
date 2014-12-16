//= require ./static-config
//= require ./dispatcher

(function () {
"use strict";

var Dispatcher = Dashboard.Dispatcher;
var config = Dashboard.config;
var fetchedProperties = [];

Dashboard.config.fetch = function () {
	var resolve, reject;
	var promise = new Promise(function (rs, rj) {
		resolve = rs;
		reject = rj;
	});

	var handleFailure = function (res, xhr) {
		config.err = new Error("SERVICE_UNAVAILABLE");

		Dispatcher.handleAppEvent({
			name: "SERVICE_UNAVAILABLE",
			status: xhr.status
		});

		reject(config.err);
	};

	var handleSuccess = function (res) {
		config.err = null;

		// clear all fetched properties from config
		fetchedProperties.forEach(function (k) {
			delete config[k];
		});
		fetchedProperties = [];

		// add fetched properties to config
		for (var k in res) {
			if (res.hasOwnProperty(k)) {
				fetchedProperties.push(k);
				config[k] = res[k];
			}
		}

		// make all endpoints absolute URLs
		var endpoints = config.endpoints;
		for (k in endpoints) {
			if (endpoints.hasOwnProperty(k) && endpoints[k][0] === "/") {
				endpoints[k] = config.API_SERVER + endpoints[k];
			}
		}

		var authenticated = res.hasOwnProperty("user");
		var authenticatedChanged = false;
		if (authenticated !== config.authenticated) {
			authenticatedChanged = true;
			config.authenticated = authenticated;
		}

		var githubAuthenticated = !!(res.user && res.user.auths && res.user.auths.hasOwnProperty("github"));
		var githubAuthenticatedChanged = false;
		if (githubAuthenticated !== config.githubAuthenticated) {
			githubAuthenticatedChanged = true;
			config.githubAuthenticated = githubAuthenticated;
		}

		Dispatcher.handleAppEvent({
			name: "CONFIG_READY"
		});

		if (authenticatedChanged) {
			Dispatcher.handleAppEvent({
				name: "AUTH_CHANGE",
				authenticated: authenticated
			});
		}

		if (githubAuthenticatedChanged) {
			Dispatcher.handleAppEvent({
				name: "GITHUB_AUTH_CHANGE",
				authenticated: githubAuthenticated
			});
		}

		resolve(config);
	};

	Marbles.HTTP({
		method: 'GET',
		url: Dashboard.config.API_SERVER + "/config",
		middleware: [
			Marbles.HTTP.Middleware.WithCredentials,
			Marbles.HTTP.Middleware.SerializeJSON
		],
		callback: function (res, xhr) {
			if (xhr.status !== 200 || !String(xhr.getResponseHeader('Content-Type')).match(/application\/json/)) {
				handleFailure(res, xhr);
			} else {
				handleSuccess(res, xhr);
			}
		}
	});

	return promise;
};

Dashboard.config.setGithubToken = function (token) {
	Dashboard.config.user.auths.github = { access_token: token };
	Dispatcher.handleAppEvent({
		name: "GITHUB_AUTH_CHANGE",
		authenticated: true
	});
};

})();
