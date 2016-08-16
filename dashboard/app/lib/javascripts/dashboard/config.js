import { extend } from 'marbles/utils';
import HTTP from 'marbles/http';
import WithCredentialsMiddleware from 'marbles/http/middleware/with_credentials';
import SerializeJSONMiddleware from 'marbles/http/middleware/serialize_json';
import StaticConfig from './static-config';
import Dispatcher from './dispatcher';

var Config = extend({
	waitForRouteHandler: Promise.resolve(),
	client: null,
	githubClient: null
}, StaticConfig);
var fetchedProperties = [];

Config.fetch = function () {
	var resolve, reject;
	var promise = new Promise(function (rs, rj) {
		resolve = rs;
		reject = rj;
	});

	var handleFailure = function (res, xhr) {
		Config.err = new Error("SERVICE_UNAVAILABLE");

		Dispatcher.handleAppEvent({
			name: "SERVICE_UNAVAILABLE",
			status: xhr.status
		});

		reject(Config.err);
	};

	var handleSuccess = function (res) {
		Config.err = null;

		// clear all fetched properties from Config
		fetchedProperties.forEach(function (k) {
			delete Config[k];
		});
		fetchedProperties = [];

		// add fetched properties to Config
		for (var k in res) {
			if (res.hasOwnProperty(k)) {
				fetchedProperties.push(k);
				Config[k] = res[k];
			}
		}

		// make all endpoints absolute URLs
		var endpoints = Config.endpoints;
		for (k in endpoints) {
			if (endpoints.hasOwnProperty(k) && endpoints[k][0] === "/") {
				endpoints[k] = Config.API_SERVER + endpoints[k];
			}
		}

		var authenticated = res.hasOwnProperty("user");
		var authenticatedChanged = false;
		if (authenticated !== Config.authenticated) {
			authenticatedChanged = true;
			Config.authenticated = authenticated;
		}

		var githubAuthenticated = !!(res.user && res.user.auths && res.user.auths.hasOwnProperty("github"));
		var githubAuthenticatedChanged = false;
		if (githubAuthenticated !== Config.githubAuthenticated) {
			githubAuthenticatedChanged = true;
			Config.githubAuthenticated = githubAuthenticated;
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

		resolve(Config);
	};

	HTTP({
		method: 'GET',
		url: Config.API_SERVER.replace(/^https?:/, window.location.protocol) + "/config",
		middleware: [
			WithCredentialsMiddleware,
			SerializeJSONMiddleware
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

Config.setGithubToken = function (token) {
	if (token) {
		Config.githubAuthenticated = true;
		Config.user.auths.github = { access_token: token };
		Dispatcher.handleAppEvent({
			name: "GITHUB_AUTH_CHANGE",
			authenticated: true
		});
	} else {
		Config.githubAuthenticated = false;
		Config.user.auths.github = null;
		Dispatcher.handleAppEvent({
			name: "GITHUB_AUTH_CHANGE",
			authenticated: false
		});
	}
};

Config.setClient = function (client) {
	Config.client = client;
};

Config.setGithubClient = function (client) {
	Config.githubClient = client;
};

Config.setDashboardAppID = function (appID) {
	Config.dashboardAppID = appID;
};

Config.freezeNav = function () {
	Config.isNavFrozen = true;
};

Config.unfreezeNav = function () {
	Config.isNavFrozen = false;
};
export default Config;
