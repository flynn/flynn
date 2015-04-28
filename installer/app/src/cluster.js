import { createClass } from 'marbles/utils';
import State from 'marbles/state';
import Config from './config';
import Dispatcher from './dispatcher';

var Cluster = createClass({
	registerWithDispatcher: function (dispatcher) {
		this.dispatcherIndex = dispatcher.register(this.handleEvent.bind(this));
	},

	willInitialize: function (attrs) {
		this.registerWithDispatcher(Dispatcher);
		this.__installState = Object.create(State);
		this.__installState.state = this.__computeInstallState(attrs);
		this.__installState.__changeListeners = [];
		this.__parseAttributes(attrs);
	},

	__computeInstallState: function (attrs) {
		if (attrs.selectedCloud === null) {
			attrs.selectedCloud = 'aws';
		}
		var prevState = this.getInstallState() || {
			logEvents: [],
			selectedCloud: 'aws',
			credentialID: null,
			certVerified: false,
			domainName: null,
			caCert: null,
			dashboardLoginToken: null,
			prompt: null,
			deleting: false,
			failed: false,
			errorDismissed: false,
			__credentials: [],
			regions: [],
			selectedRegionSlug: 'nyc3',
			selectedSizeSlug: '2gb',
			numInstances: 1,
		};
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

			__credentials: attrs.credentials || prevState.__credentials,
			regions: attrs.regions || prevState.regions,
			selectedRegionSlug: attrs.selectedRegionSlug || prevState.selectedRegionSlug,
			selectedRegion: null,
			selectedSizeSlug: attrs.selectedSizeSlug || prevState.selectedSizeSlug,
			numInstances: attrs.num_instances || prevState.numInstances,

			prompt: prevState.prompt
		};

		if (state.selectedRegionSlug === null && state.regions.length > 0) {
			state.selectedRegionSlug = state.regions[0].slug;
		}

		for (var i = 0, len = state.regions.length; i < len; i++) {
			if (state.regions[i].slug === state.selectedRegionSlug) {
				state.selectedRegion = state.regions[i];
				break;
			}
		}

		if (state.selectedSizeSlug === null && state.selectedRegion) {
			state.selectedSizeSlug = state.selectedRegion.sizes[0];
		}

		var credentialIDExists = false;
		state.credentials = state.__credentials.filter(function (creds) {
			var keep = creds.type === state.selectedCloud;
			if (keep && creds.id === state.credentialID) {
				credentialIDExists = true;
			}
			return keep;
		});

		if ( !credentialIDExists ) {
			state.credentialID = null;
		}

		if (state.credentialID === null && state.selectedCloud === 'aws' && Config.has_aws_env_credentials) {
			state.credentialID = 'aws_env';
		}

		if (state.credentialID === null && state.credentials.length > 0) {
			state.credentialID = state.credentials[0].id;
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

	getInstallState: function () {
		return this.__installState.state;
	},

	toJSON: function () {
		var data = {};
		var jsonFields = this.constructor.jsonFields;
		for (var k in jsonFields) {
			if (this.hasOwnProperty(jsonFields[k])) {
				data[k] = this[jsonFields[k]];
			}
		}
		return data;
	},

	__setInstallState: function (newState, shouldDelay) {
		var prevCredentialID = this.__installState.state.credentialID;
		if (newState.credentialID !== prevCredentialID && newState.selectedCloud === 'digital_ocean') {
			Dispatcher.dispatch({
				name: 'SELECTED_CREDENTIAL_ID_CHANGE',
				credentialID: newState.credentialID,
				cloud: newState.selectedCloud
			});
		}
		if (shouldDelay) {
			this.__installState.setStateWithDelay(newState, 25);
		} else {
			this.__installState.setState(newState);
		}
	},

	__parseAttributes: function (attrs) {
		var jsonFields = this.constructor.jsonFields;
		for (var k in jsonFields) {
			if (attrs.hasOwnProperty(k)) {
				this[jsonFields[k]] = attrs[k];
			}
		}
		this.name = this.ID;
	},

	addChangeListener: function () {
		this.__installState.addChangeListener.apply(this.__installState, arguments);
	},

	removeChangeListener: function () {
		this.__installState.removeChangeListener.apply(this.__installState, arguments);
	},

	handleEvent: function (event) {
		if (event.clusterID !== this.ID) {
			return;
		}

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
				this.__setInstallState(
					this.__computeInstallState({state: 'error', errorDismissed: true})
				);
			break;

			case 'CLUSTER_STATE':
				this.__setInstallState(
					this.__computeInstallState({state: event.state})
				);
			break;

			case 'INSTALL_PROMPT_REQUESTED':
				this.__setInstallState({
					prompt: event.prompt
				});
			break;

			case 'INSTALL_PROMPT_RESOLVED':
				if (event.prompt.id === (this.getInstallState().prompt || {}).id) {
					this.__setInstallState({
						prompt: null
					});
				}
			break;

			case 'INSTALL_DONE':
				this.__parseAttributes(event.cluster);
				this.__setInstallState(this.__computeInstallState(event.cluster));
			break;

			case 'CERT_VERIFIED':
				this.__setInstallState({
					certVerified: true
				});
			break;

			case 'SELECT_CLOUD':
				this.__setInstallState(this.__computeInstallState({
					selectedCloud: event.cloud
				}));
			break;

			case 'SELECT_CREDENTIAL':
				this.__setInstallState(this.__computeInstallState({
					credentialID: event.credentialID
				}));
			break;

			case 'CREDENTIALS_CHANGE':
				this.__setInstallState(this.__computeInstallState({
					credentials: event.credentials
				}));
			break;

			case 'CLOUD_REGIONS':
				var state = this.getInstallState();
				if (state.credentialID !== event.credentialID || state.selectedCloud !== event.cloud) {
					return;
				}
				this.__setInstallState(this.__computeInstallState({
					regions: event.regions.sort(function (a, b) {
						return a.name.localeCompare(b.name);
					})
				}));
			break;

			case 'SELECT_REGION':
				this.__setInstallState(this.__computeInstallState({
					selectedRegionSlug: event.slug
				}));
			break;

			case 'SELECT_SIZE':
				this.__setInstallState(this.__computeInstallState({
					selectedSizeSlug: event.slug
				}));
			break;

			case 'SELECT_NUM_INSTANCES':
				this.__setInstallState(this.__computeInstallState({
					num_instances: event.numInstances
				}));
			break;
		}
	},

	__addLog: function (data) {
		var logEvents = [].concat(this.getInstallState().logEvents);
		logEvents.push(data);
		this.__setInstallState({
			logEvents: logEvents
		}, true);
	}
});

Cluster.jsonFields = {
	id: 'ID',
	type: 'type',
	state: 'state',
	region: 'region',
	size: 'size',
	num_instances: 'numInstances',
	instance_type: 'instanceType',
	controller_key: 'controllerKey',
	controller_pin: 'controllerPin',
	dashboard_login_token: 'dashboardLoginToken',
	domain: 'domain',
	ca_cert: 'caCert',
	vpc_cidr: 'vpcCidr',
	subnetCidr: 'subnetCidr',
	credential_id: 'credentialID'
};

export default Cluster;
