import { extend, createClass } from 'marbles/utils';
import State from 'marbles/state';
import Dispatcher from './dispatcher';
import Config from './config';

var releaseChannels = Config.release_channels || [];
if (releaseChannels.length === 0) {
	releaseChannels = [{name: null, version: null}];
}

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
		this.setState(this.__computeState(extend({
			credentials: attrs.credentials
		}, this.attrs)));
	},

	getInitialState: function () {
		return {
			releaseChannel: releaseChannels[0].name,
			releaseVersion: releaseChannels[0].version,
			selectedCloud: this.constructor.type,
			selectedRegionSlug: null,
			logEvents: [],
			credentialID: null,
			certVerified: false,
			domainName: null,
			caCert: null,
			dashboardLoginToken: null,
			deleting: false,
			failed: false,
			regions: [],
			numInstances: 1,
			credentials: [],
			__allCredentials: [],
			backupFile: null
		};
	},

	__computeState: function (attrs) {
		var prevState = this.state;
		var state = {
			inProgress: false,
			deleting: prevState.deleting,
			failed: prevState.failed,
			logEvents: prevState.logEvents,
			steps: [
				{ id: 'configure', label: 'Configure', complete: false },
				{ id: 'install',   label: 'Install',   complete: false },
				{ id: 'dashboard', label: 'Dashboard', complete: false },
				{ id: 'done', visible: false }
			],
			selectedCloud: attrs.selectedCloud || attrs.type || prevState.selectedCloud,
			credentialID: attrs.credentialID || prevState.credentialID,
			certVerified: attrs.certVerified || prevState.certVerified,

			domainName: attrs.domain ? attrs.domain.domain : prevState.domainName,
			caCert: attrs.ca_cert ? window.btoa(attrs.ca_cert) : prevState.caCert,
			dashboardLoginToken: attrs.dashboard_login_token || prevState.dashboardLoginToken,

			regions: attrs.regions || prevState.regions,
			numInstances: attrs.num_instances || prevState.numInstances,

			backupFile: attrs.backupFile || prevState.backupFile,

			selectedRegionSlug: attrs.selectedRegionSlug || prevState.selectedRegionSlug,
			selectedRegion: null,

			errorMessage: attrs.hasOwnProperty('errorMessage') ? attrs.errorMessage : prevState.errorMessage,

			__allCredentials: attrs.credentials || prevState.__allCredentials
		};

		if (attrs.hasOwnProperty('releaseChannel') && attrs.releaseChannel !== prevState.releaseChannel) {
			state.releaseChannel = attrs.releaseChannel;
		} else {
			state.releaseChannel = prevState.releaseChannel;
		}
		if (attrs.hasOwnProperty('releaseVersion') && attrs.releaseVersion !== prevState.releaseVersion) {
			state.releaseVersion = attrs.releaseVersion;
		} else {
			state.releaseVersion = prevState.releaseVersion;
		}

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

		state.currentStep = 'configure';
		var clusterState = attrs.state || this.attrs.state;
		switch (clusterState) {
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
			if (this.attrs.ID !== 'new') {
				state.currentStep = 'install';
			}
			break;

		case 'running':
			if (state.certVerified) {
				state.currentStep = 'done';
			} else {
				state.currentStep = 'dashboard';
			}
			break;
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

	setCredentials: function (credentials) {
		this.setState(this.__computeState({
			state: this.attrs.state,
			credentials: credentials
		}));
	},

	setAttrs: function (attrs) {
		this.__parseAttributes(attrs);
		this.setState(this.__computeState(this.attrs));
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

	getBackupFile: function () {
		return this.state.backupFile;
	},

	handleEvent: function (event) {
		if (event.name === 'LAUNCH_CLUSTER' && this.attrs.ID === 'new') {
			this.setState(this.__computeState({
				errorMessage: null
			}));
		}

		if (event.clusterID !== this.attrs.ID) {
			return;
		}

		var state;

		switch (event.name) {
		case 'LAUNCH_CLUSTER_FAILURE':
			this.setState(this.__computeState({
				state: 'error',
				errorMessage: (event.res || {}).message || ('Something went wrong ('+ ((event.xhr || {}).status || '0') +')')
			}));
			break;

		case 'LOG':
			this.__addLog(event.data);
			break;

		case 'INSTALL_ERROR':
			this.__addLog({
				description: 'Error: '+ event.message
			});
			break;

		case 'CLUSTER_STATE':
			this.setState(
				this.__computeState({state: event.state})
			);
			break;

		case 'INSTALL_DONE':
			this.__parseAttributes(event.cluster);
			this.setState(this.__computeState(event.cluster));
			break;

		case 'CERT_VERIFIED':
			this.setState(this.__computeState({
				certVerified: true
			}));
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

		case 'SELECT_RELEASE_CHANNEL':
			this.setState(this.__computeState({
				releaseChannel: event.releaseChannel,
				releaseVersion: event.releaseVersion
			}));
			break;

		case 'SELECT_RELEASE_VERSION':
			this.setState(this.__computeState({
				releaseChannel: event.releaseChannel,
				releaseVersion: event.releaseVersion
			}));
			break;

		case 'SELECT_BACKUP':
			this.setState(this.__computeState({
				backupFile: event.file
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
	this.attrs.releaseChannel = newState.releaseChannel;
	this.attrs.releaseVersion = newState.releaseVersion;
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
	release_channel: 'releaseChannel',
	release_version: 'releaseVersion',
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
