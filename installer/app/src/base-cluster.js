import { createClass } from 'marbles/utils';
import State from 'marbles/state';
import Dispatcher from './dispatcher';

var BaseCluster = createClass({
	mixins: [State],

	__changeListeners: [],

	registerWithDispatcher: function (dispatcher) {
		this.dispatcherIndex = dispatcher.register(this.handleEvent.bind(this));
	},

	willInitialize: function (attrs) {
		this.registerWithDispatcher(Dispatcher);
		this.type = this.constructor.type;
		this.attrs = {};
		this.__parseAttributes(attrs);
		this.state = this.getInitialState();
		this.setState(this.__computeState(attrs));
	},

	getInitialState: function () {
		return {
			selectedCloud: this.constructor.type,
			selectedRegionSlug: null,
			logEvents: [],
			credentialID: null,
			certVerified: false,
			domainName: null,
			caCert: null,
			dashboardLoginToken: null,
			prompt: null,
			deleting: false,
			failed: false,
			errorDismissed: false,
			regions: [],
			numInstances: 1,
			credentials: [],
			__allCredentials: []
		};
	},

	__computeState: function (attrs) {
		var prevState = this.state;
		var state = {
			inProgress: false,
			deleting: prevState.deleting,
			failed: prevState.failed,
			errorDismissed: prevState.errorDismissed,
			logEvents: prevState.logEvents,
			steps: [
				{ id: 'configure', label: 'Configure', complete: false },
				{ id: 'install',   label: 'Install',   complete: false },
				{ id: 'dashboard', label: 'Dashboard', complete: false }
			],
			currentStep: 'configure',
			selectedCloud: attrs.selectedCloud || attrs.type || prevState.selectedCloud,
			credentialID: attrs.credentialID || prevState.credentialID,
			certVerified: attrs.certVerified || prevState.certVerified,

			domainName: attrs.domain ? attrs.domain.domain : prevState.domainName,
			caCert: attrs.ca_cert ? window.btoa(attrs.ca_cert) : prevState.caCert,
			dashboardLoginToken: attrs.dashboard_login_token || prevState.dashboardLoginToken,

			regions: attrs.regions || prevState.regions,
			numInstances: attrs.num_instances || prevState.numInstances,

			selectedRegionSlug: attrs.selectedRegionSlug || prevState.selectedRegionSlug,
			selectedRegion: null,

			prompt: prevState.prompt,

			__allCredentials: attrs.credentials || prevState.__allCredentials
		};

		var credentialIDExists = false;
		state.credentials = state.__allCredentials.filter(function (creds) {
			var keep = creds.type === state.selectedCloud;
			if (keep && creds.id === state.credentialID) {
				credentialIDExists = true;
			}
			return keep;
		});

		if ( !credentialIDExists ) {
			state.credentialID = null;
		}

		if (state.selectedRegionSlug === null) {
			state.selectedRegionSlug = this.constructor.defaultRegionSlug;
		}
		if (state.selectedRegionSlug === null && state.regions.length > 0) {
			state.selectedRegionSlug = state.regions[0].slug;
		}
		for (var i = 0, len = state.regions.length; i < len; i++) {
			if (state.regions[i].slug === state.selectedRegionSlug) {
				state.selectedRegion = state.regions[i];
				break;
			}
		}

		switch (attrs.state) {
			case 'starting':
				state.currentStep = 'install';
				state.inProgress = true;
			break;

			case 'deleting':
				state.currentStep = 'install';
				state.deleting = true;
			break;

			case 'error':
				state.failed = true;
				state.currentStep = 'install';
			break;

			case 'running':
				state.currentStep = 'dashboard';
			break;
		}

		if (attrs.errorDismissed) {
			state.errorDismissed = true;
		}

		var complete = true;
		state.steps.forEach(function (step) {
			if (step.id === state.currentStep) {
				complete = false;
			}
			step.complete = complete;
		});

		return state;
	},

	__parseAttributes: function (data) {
		var jsonFields = this.constructor.jsonFields;
		var attrs = {};
		for (var k in jsonFields) {
			if (data.hasOwnProperty(k)) {
				attrs[jsonFields[k]] = data[k];
			}
		}
		attrs.name = attrs.ID;
		this.attrs = attrs;
	},

	toJSON: function () {
		var data = {};
		var attrs = this.attrs;
		var jsonFields = this.constructor.jsonFields;
		for (var k in jsonFields) {
			if (attrs.hasOwnProperty(jsonFields[k])) {
				data[k] = attrs[jsonFields[k]];
			}
		}
		data.type = this.constructor.type;
		return data;
	},

	handleEvent: function (event) {
		if (event.clusterID !== this.attrs.ID) {
			return;
		}

		var state;

		switch (event.name) {
			case 'LOG':
				this.__addLog(event.data);
			break;

			case 'INSTALL_ERROR':
				this.__addLog({
					description: 'Error: '+ event.message
				});
			break;

			case 'INSTALL_ERROR_DISMISS':
				this.setState(
					this.__computeState({state: 'error', errorDismissed: true})
				);
			break;

			case 'CLUSTER_STATE':
				this.setState(
					this.__computeState({state: event.state})
				);
			break;

			case 'INSTALL_PROMPT_REQUESTED':
				this.setState({
					prompt: event.prompt
				});
			break;

			case 'INSTALL_PROMPT_RESOLVED':
				if (event.prompt.id === (this.state.prompt || {}).id) {
					this.setState({
						prompt: null
					});
				}
			break;

			case 'INSTALL_DONE':
				this.__parseAttributes(event.cluster);
				this.setState(this.__computeState(event.cluster));
			break;

			case 'CERT_VERIFIED':
				this.setState({
					certVerified: true
				});
			break;

			case 'SELECT_CREDENTIAL':
				this.setState(this.__computeState({
					credentialID: event.credentialID
				}));
			break;

			case 'CREDENTIALS_CHANGE':
				this.setState(this.__computeState({
					credentials: event.credentials
				}));
			break;

			case 'CLOUD_REGIONS':
				state = this.state;
				if (state.credentialID !== event.credentialID || state.selectedCloud !== event.cloud) {
					return;
				}
				this.setState(this.__computeState({
					regions: event.regions.sort(function (a, b) {
						return a.name.localeCompare(b.name);
					})
				}));
			break;

			case 'SELECT_REGION':
				this.setState(this.__computeState({
					selectedRegionSlug: event.region
				}));
			break;

			case 'SELECT_SIZE':
				this.setState(this.__computeState({
					selectedSizeSlug: event.slug
				}));
			break;

			case 'SELECT_NUM_INSTANCES':
				this.setState(this.__computeState({
					num_instances: event.numInstances
				}));
			break;
		}
	},

	__addLog: function (data) {
		var logEvents = [].concat(this.state.logEvents);
		logEvents.push(data);
		this.setState({
			logEvents: logEvents
		}, true);
	}
});

BaseCluster.prototype.setState = function (newState, shouldDelay) {
	this.attrs.credentialID = newState.credentialID;
	this.attrs.numInstances = newState.numInstances;
	this.attrs.region = newState.selectedRegionSlug;

	if (shouldDelay) {
		State.setStateWithDelay.call(this, newState, 25);
	} else {
		State.setState.call(this, newState);
	}
};


BaseCluster.jsonFields = {
	id: 'ID',
	type: 'type',
	state: 'state',
	region: 'region',
	num_instances: 'numInstances',
	controller_key: 'controllerKey',
	controller_pin: 'controllerPin',
	dashboard_login_token: 'dashboardLoginToken',
	domain: 'domain',
	ca_cert: 'caCert',
	credential_id: 'credentialID'
};

var clusterTypes = {};
var baseClusterCreateClass = BaseCluster.createClass;
BaseCluster.createClass = function (type, proto) {
	var klass = baseClusterCreateClass.call(this, proto);
	klass.type = type;
	clusterTypes[type] = klass;
	return klass;
};

BaseCluster.newOfType = function (type, attrs) {
	if (clusterTypes.hasOwnProperty(type)) {
		return new clusterTypes[type](attrs);
	}
	throw new Error("Unknown cluster type: "+ JSON.stringify(type));
};

export default BaseCluster;
