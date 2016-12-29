import { extend } from 'marbles/utils';
import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';

var provisionResource = function (providerID, createRoute) {
	var client = Config.client;
	client.provisionResource(providerID).then(function (args) {
		var resource = args[0];
		if ( !createRoute ) {
			return;
		}
		return createTCPRoute(createRoute.serviceAppIDFromResource(resource), createRoute.serviceNameFromResource(resource));
	});
};

var copyResourceEnvToApp = function (appID, resourceEnv) {
	var client = Config.client;
	resourceEnv = resourceEnv || {};
	client.getAppRelease(appID).catch(function (args) {
		var xhr = (args || [])[1];
		if (xhr && xhr.status === 404) {
			// app doesn't have a release yet
			return [{}, null];
		}
		return Promise.reject(args);
	}).then(function (args) {
		var release = extend({app_id: appID}, args[0]);
		release.env = release.env || {};
		Object.keys(resourceEnv).forEach(function (k) {
			if (release.env.hasOwnProperty(k) && release.env[k] !== resourceEnv[k]) {
				// add suffix to prevent overwriting
				var suffix = 1;
				Object.keys(release.env).forEach(function (rk) {
					if (rk.substr(0, k.length) !== k) {
						return;
					}
					var s = rk.substr(k.length);
					var m = s.match(/(\d+)$/);
					if (m && parseInt(m[1], 10) >= suffix) {
						suffix = parseInt(m[1], 10) + 1;
					}
				});
				release.env[k +'_'+ suffix] = resourceEnv[k];
			} else {
				release.env[k] = resourceEnv[k];
			}
		});
		delete release.id;
		delete release.created_at;
		return client.createRelease(release).then(function (args) {
			var release = args[0];
			return client.deployAppRelease(appID, release.id);
		});
	});
};

var provisionResourcesForApp = function (providerIDs, appID) {
	var client = Config.client;
	var env = {};
	Promise.all(providerIDs.map(function (providerID) {
		return client.provisionResource(providerID, {
			apps: [appID]
		}).then(function (args) {
			var resource = args[0];
			extend(env, resource.env);
		});
	})).then(function () {
		return copyResourceEnvToApp(appID, env);
	}).catch(function (args) {
		Dispatcher.dispatch({
			name: 'APP_PROVISION_RESOURCES_FAILED',
			errMsg: (args[0] || {}).message || ('Something went wrong ('+ (args[1] || {}).status +')')
		});
	});
};

var addAppToResource = function (appID, providerID, resourceID) {
	var client = Config.client;
	client.getResource(providerID, resourceID).then(function (args) {
		var resource = args[0];
		return copyResourceEnvToApp(appID, resource.env);
	}).then(function () {
		return client.addResourceApp(providerID, resourceID, appID);
	}).catch(function (args) {
		Dispatcher.dispatch({
			name: 'RESOURCE_ADD_APP_FAILED',
			errMsg: (args[0] || {}).message || ('Something went wrong ('+ (args[1] || {}).status +')')
		});
	});
};

var deleteResource = function (providerID, resourceID, appID) {
	var client = Config.client;
	var shouldDeleteResource = true;
	client.getResource(providerID, resourceID).then(function (args) {
		// remove resource env from all associated apps or just appID if given
		var resource = args[0];
		var resourceEnv = resource.env || {};
		var appIDs = appID ? [appID] : resource.apps || [];
		if (appID && (resource.apps || []).length > 1) {
			shouldDeleteResource = false;
		}
		return Promise.all(appIDs.map(function (appID) {
			return client.getAppRelease(appID).then(function (args) {
				var release = extend({}, args[0]);
				var newReleaseEnv = extend({}, release.env || {});
				var findReleaseEnvKey = function (k) {
					for (var rk in newReleaseEnv) {
						if ( !newReleaseEnv.hasOwnProperty(rk) ) {
							continue;
						}
						if (rk.substr(0, k.length) === k && newReleaseEnv[rk] === resourceEnv[k]) {
							return rk;
						}
					}
					return null;
				};
				Object.keys(resourceEnv).forEach(function (k) {
					var rk = findReleaseEnvKey(k);
					if (newReleaseEnv[rk] === resourceEnv[k]) {
						delete newReleaseEnv[rk];
					}
				});
				release.env = newReleaseEnv;
				delete release.id;
				delete release.created_at;

				return client.createRelease(release).then(function (args) {
					var release = args[0];
					return client.deployAppRelease(appID, release.id);
				});
			}).catch(function () {
				// app doesn't have a release, ignore
			});
		})).then(function () {
			if (shouldDeleteResource) {
				return;
			}
			// the resource has other apps using it
			// so remove appID from resource.apps instead of deleting it
			return client.deleteResourceApp(providerID, resourceID, appID);
		});
	}).then(function () {
		if (shouldDeleteResource) {
			return client.deleteResource(providerID, resourceID);
		}
	});
};

var createTCPRoute = function (serviceAppID, serviceName) {
	var client = Config.client;
	client.createAppRoute(serviceAppID, {
		type: 'tcp',
		leader: true,
		service: serviceName
	});
};

Dispatcher.register(function (event) {
	switch (event.name) {
	case 'PROVISION_RESOURCE_WITH_ROUTE':
		if (event.resourceID) {
			Config.history.navigate('/providers/'+ event.providerID +'/resources/'+ event.resourceID +'/create-external-route?provision=true', { replace: true });
		} else {
			Config.history.navigate('/providers/'+ event.providerID +'/create-external-route?provision=true', { replace: true });
		}
		break;
	case 'PROVISION_RESOURCE':
		provisionResource(event.providerID, event.createRoute || null);
		break;
	case 'APP_PROVISION_RESOURCES':
		provisionResourcesForApp(event.providerIDs, event.appID);
		break;
	case 'RESOURCE_ADD_APP':
		addAppToResource(event.appID, event.providerID, event.resourceID);
		break;
	case 'DELETE_RESOURCE':
		deleteResource(event.providerID, event.resourceID, event.appID);
		break;
	case 'CREATE_EXTERNAL_PROVIDER_ROUTE':
		createTCPRoute(event.providerAppID, event.serviceName);
		break;
	case 'CREATE_EXTERNAL_RESOURCE_ROUTE':
		Config.client.getResource(event.providerID, event.resourceID).then(function (args) {
			var resource = args[0];
			createTCPRoute(event.serviceAppID, event.serviceNameFromResource(resource));
		});
		break;
	}
});
