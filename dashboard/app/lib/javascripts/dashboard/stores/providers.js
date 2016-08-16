import { extend } from 'marbles/utils';
import State from 'marbles/state';
import Store from 'dashboard/store';
import Config from 'dashboard/config';
import Dispatcher from 'dashboard/dispatcher';

var resourceAppsKey = function (providerID, resourceID) {
	return providerID +':'+ resourceID;
};

var providerIDFromResourceAppsKey = function (key) {
	return key.split(':')[0];
};

var Providers = Store.createClass({
	displayName: "Stores.Providers",

	getState: function () {
		return this.state;
	},

	didBecomeActive: function () {
		this.__fetchProviders();
	},

	getInitialState: function () {
		return {
			fetched: false,
			providers: [],
			newResourceStates: {},
			deletingResourceStates: {},
			newRouteStates: {},
			providerApps: {},
			resourceApps: {}
		};
	},

	handleEvent: function (event) {
		this.__resolveWaitForEvents(event.name);
		switch (event.name) {
		case 'PROVISION_RESOURCE':
			this.setState({
				newResourceStates: extend({}, this.state.newResourceStates, (function () {
					var s = {};
					s[event.providerID] = {
						isCreating: true,
						shouldCreateRoute: !!event.createRoute,
						errMsg: null
					};
					return s;
				})())
			});
			break;

		case 'APP_PROVISION_RESOURCES':
			this.setState({
				newResourceStates: extend({}, this.state.newResourceStates, (function () {
					var s = {};
					event.providerIDs.forEach(function (providerID) {
						s[providerID] = {
							isCreating: true,
							errMsg: null
						};
					});
					return s;
				})())
			});
			break;

		case 'PROVISION_RESOURCE_FAILED':
			this.setState({
				newResourceStates: extend({}, this.state.newResourceStates, (function () {
					var s = {};
					s[event.providerID] = {
						isCreating: false,
						errMsg: event.error
					};
					return s;
				})())
			});
			break;

		case 'RESOURCE':
			this.__handleResourceEvent(event);
			break;

		case 'DELETE_RESOURCE':
			this.__handleDeleteResourceEvent(event);
			break;

		case 'DELETE_RESOURCE_FAILED':
			this.__handleDeleteResourceFailedEvent(event);
			break;

		case 'RESOURCE_DELETED':
			this.__handleResourceDeletedEvent(event);
			break;

		case 'CREATE_EXTERNAL_PROVIDER_ROUTE':
		case 'CREATE_EXTERNAL_RESOURCE_ROUTE':
			this.setState({
				newRouteStates: extend({}, this.state.newRouteStates, (function () {
					var s = {};
					s[event.providerID] = {
						errMsg: null
					};
					return s;
				})())
			});
			break;

		case 'ROUTE':
			this.__handleRouteEvent(event);
			break;

		case 'CREATE_APP_ROUTE_FAILED':
			this.__handleCreateAppRouteFailedEvent(event);
			break;

		case 'APP':
			this.__waitForEvent('RESOURCE').then(this.__handleAppEvent.bind(this, event));
			break;
		}
	},

	__waitForEventIndex: {},
	__waitForEvent: function (eventName) {
		var resolve;
		var promise = new Promise(function (rs) {
			resolve = rs;
		});
		this.__waitForEventIndex[eventName] = this.__waitForEventIndex[eventName] || [];
		this.__waitForEventIndex[eventName].push(resolve);
		return promise;
	},

	__resolveWaitForEvents: function (eventName) {
		if ( !this.__waitForEventIndex.hasOwnProperty(eventName) ) {
			return;
		}
		this.__waitForEventIndex[eventName].forEach(function (fn) {
			fn();
		});
		delete this.__waitForEventIndex[eventName];
	},

	__handleResourceEvent: function (event) {
		var provider = null;
		var providers = this.state.providers;
		for (var i = 0, len = providers.length; i < len; i++) {
			if (providers[i].id === event.data.provider) {
				provider = providers[i];
				break;
			}
		}
		if (provider === null || !this.state.newResourceStates.hasOwnProperty(provider.id)) {
			return;
		}
		var newResourceStates = extend({}, this.state.newResourceStates);
		if (Config.PROVIDER_ATTRS[provider.name].route.mode === 'resource') {
			newResourceStates[provider.id] = extend({}, newResourceStates[provider.id], {
				resourceAppName: Config.PROVIDER_ATTRS[provider.name].route.appNameFromResource(event.data),
				resourceID: event.object_id
			});
		}
		if ( !newResourceStates[provider.id].shouldCreateRoute ) {
			if (Config.PROVIDER_ATTRS[provider.name].route.mode === 'resource') {
				newResourceStates[provider.id].isCreating = false;
				newResourceStates[provider.id].errMsg = null;
			} else {
				delete newResourceStates[provider.id];
			}
		}
		this.setState({
			newResourceStates: newResourceStates
		});
	},

	__handleDeleteResourceEvent: function (event) {
		this.setState({
			deletingResourceStates: extend({}, this.state.deletingResourceStates, (function () {
				var s = {};
				s[event.resourceID] = {
					isDeleting: true,
					errMsg: null
				};
				return s;
			})())
		});
	},

	__handleDeleteResourceFailedEvent: function (event) {
		this.setState({
			deletingResourceStates: extend({}, this.state.deletingResourceStates, (function () {
				var s = {};
				s[event.resourceID] = {
					isDeleting: false,
					errMsg: event.error
				};
				return s;
			})())
		});
	},

	__handleResourceDeletedEvent: function (event) {
		if ( !this.state.deletingResourceStates.hasOwnProperty(event.object_id) ) {
			return;
		}
		this.setState({
			deletingResourceStates: extend({}, this.state.deletingResourceStates, (function () {
				var s = {};
				s[event.object_id] = {
					isDeleting: false,
					errMsg: null
				};
				return s;
			})())
		});
	},

	__handleRouteEvent: function (event) {
		var newRouteStates = extend({}, this.state.newRouteStates, (function () {
			var s = {};
			var providerID = null;
			var providerApps = this.state.providerApps;
			for (var pID in providerApps) {
				if ( !providerApps.hasOwnProperty(pID) ) {
					continue;
				}
				if (providerApps[pID].id === event.app) {
					providerID = pID;
					break;
				}
			}
			if (providerID === null) {
				return s;
			}
			s[providerID] = {
				errMsg: null
			};
			return s;
		}.bind(this))());

		var newResourceStates = extend({}, this.state.newResourceStates);
		(function () {
			var providerID = null;
			var resourceApps = this.state.resourceApps;
			for (var k in resourceApps) {
				if ( !resourceApps.hasOwnProperty(k) ) {
					continue;
				}
				if (resourceApps[k].id === event.app) {
					providerID = providerIDFromResourceAppsKey(k);
					break;
				}
			}
			if ( !newResourceStates.hasOwnProperty(providerID) ) {
				return;
			}
			if (newResourceStates[providerID].shouldCreateRoute) {
				delete newResourceStates[providerID];
			}
		}.bind(this))();

		this.setState({
			newRouteStates: newRouteStates,
			newResourceStates: newResourceStates
		});
	},

	__handleCreateAppRouteFailedEvent: function (event) {
		var errMsg = (event.error || {}).message || ('Something went wrong ('+ event.status +')');
		var newRouteStates = extend({}, this.state.newRouteStates, (function () {
			var s = {};
			var providerID = null;
			var providerApps = this.state.providerApps;
			for (var pID in providerApps) {
				if ( !providerApps.hasOwnProperty(pID) ) {
					continue;
				}
				if (providerApps[pID].id === event.appID) {
					providerID = pID;
					break;
				}
			}
			if (providerID === null) {
				return s;
			}
			s[providerID] = {
				errMsg: errMsg
			};
			return s;
		}.bind(this))());

		var newResourceStates = extend({}, this.state.newResourceStates);
		(function () {
			var providerID = null;
			var resourceApps = this.state.resourceApps;
			for (var k in resourceApps) {
				if ( !resourceApps.hasOwnProperty(k) ) {
					continue;
				}
				if (resourceApps[k].id === event.app) {
					providerID = k.split(':')[0];
					break;
				}
			}
			if ( !newResourceStates.hasOwnProperty(providerID) ) {
				return;
			}
			if (newResourceStates[providerID].shouldCreateRoute) {
				newResourceStates[providerID] = {
					isCreating: false,
					shouldCreateRoute: true,
					errMsg: errMsg
				};
			}
		}.bind(this))();

		this.setState({
			newRouteStates: newRouteStates,
			newResourceStates: newResourceStates
		});
	},

	__handleAppEvent: function (event) {
		var newResourceStates = extend({}, this.state.newResourceStates);
		var providerID = Object.keys(newResourceStates).find(function (k) {
			return newResourceStates[k].resourceAppName === event.data.name;
		});
		if ( !providerID ) {
			return;
		}
		var resourceApps = extend({}, this.state.resourceApps);
		resourceApps[resourceAppsKey(providerID, newResourceStates[providerID].resourceID)] = event.data;
		delete newResourceStates[providerID];
		this.setState({
			resourceApps: resourceApps,
			newResourceStates: newResourceStates
		});
	},

	__fetchProviders: function () {
		Config.client.listProviders().then(function (args) {
			var res = args[0];
			var providers = {};
			var providerApps = {};
			var resourceApps = {};
			Promise.all(res.map(function (provider) {
				var pAttrs = Config.PROVIDER_ATTRS[provider.name];
				if ( !pAttrs ) {
					window.console.error('No provider config found for '+ provider.name);
					return;
				}
				providers[provider.name] = provider;
				return Promise.all([
					Config.client.getApp(pAttrs.appName).then(function (args) {
						providerApps[provider.id] = args[0];
					}),
					Promise.resolve(null).then(function () {
						if (pAttrs.route.mode !== 'resource') {
							return;
						}
						return Config.client.listProviderResources(provider.id).then(function (args) {
							return Promise.all(args[0].map(function (resource) {
								var appName = pAttrs.route.appNameFromResource(resource);
								return Config.client.getApp(appName).then(function (args) {
									resourceApps[resourceAppsKey(provider.id, resource.id)] = args[0];
								});
							}));
						});
					})
				]);
			}, this)).then(function () {
				this.setState({
					fetched: true,
					providers: Config.PROVIDER_ORDER.filter(function (name) {
						return providers.hasOwnProperty(name);
					}).map(function (name) {
						return providers[name];
					}),
					providerApps: providerApps,
					resourceApps: resourceApps
				});
			}.bind(this));
		}.bind(this));
	}
}, State);

Providers.registerWithDispatcher(Dispatcher);

export default Providers;
export { resourceAppsKey };
