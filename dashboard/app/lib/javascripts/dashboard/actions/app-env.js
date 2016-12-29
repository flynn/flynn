import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';
import { extend } from 'marbles/utils';
import { objectDiff, applyObjectDiff } from 'dashboard/utils';

var updateAppEnv = function (appID, changedRelease, env, deployTimeout) {
	var client = Config.client;
	client.getAppRelease(appID).catch(function (args) {
		var xhr = (args || [])[1];
		if (xhr && xhr.status === 404) {
			// app doesn't have a release yet
			return [{}, null];
		}
		return Promise.reject(args);
	}).then(function (args) {
		var release = extend({app_id: appID}, args[0]);
		var envDiff = objectDiff(changedRelease.env || {}, env);
		release.env = applyObjectDiff(envDiff, release.env || {});
		delete release.id;
		delete release.created_at;
		
		return client.createRelease(release);
	}).then(function (args) {
		var release = args[0];
		return client.deployAppRelease(appID, release.id, deployTimeout);
	}).then(function () {
		if (appID === Config.dashboardAppID || appID === 'dashboard') {
			Config.setGithubToken(env.GITHUB_TOKEN);
		}
	}).catch(function (args) {
		var message = 'Something went wrong.';
		if (Array.isArray(args)) {
			message = args[0].message;
		} else if (typeof args === 'string') {
			message = args;
		}
		Dispatcher.dispatch({
			name: 'UPDATE_APP_ENV_FAILED',
			appID: appID,
			errorMsg: message
		});
		return Promise.reject(args);
	});
};

Dispatcher.register(function (event) {
	switch (event.name) {
	case 'UPDATE_APP_ENV':
		updateAppEnv(event.appID, event.prevRelease, event.data, event.deployTimeout);
		break;
	}
});
