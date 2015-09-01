import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';
import { extend } from 'marbles/utils';
import { objectDiff, applyObjectDiff } from 'dashboard/utils';

var updateAppEnv = function (appID, changedRelease, env) {
	var client = Config.client;
	client.getAppRelease(appID).then(function (args) {
		var release = extend({}, args[0]);
		var envDiff = objectDiff(changedRelease.env || {}, env);
		release.env = applyObjectDiff(envDiff, release.env);
		delete release.id;
		
		return client.createRelease(release);
	}).then(function (args) {
		var release = args[0];
		return client.deployAppRelease(appID, release.id);
	});
};

Dispatcher.register(function (event) {
	switch (event.name) {
		case 'UPDATE_APP_ENV':
			updateAppEnv(event.appID, event.prevRelease, event.data);
		break;
	}
});
