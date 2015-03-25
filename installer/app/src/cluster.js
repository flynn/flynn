import { createClass } from 'marbles/utils';
import State from 'marbles/state';

var Cluster = createClass({
	willInitialize: function (attrs) {
		this.__installState = Object.create(State);
		this.__installState.state = this.__computeInstallState(attrs);
		this.__installState.__changeListeners = [];
		this.__parseAttributes(attrs);
	},

	__computeInstallState: function (attrs) {
		var prevState = this.getInstallState() || {
			logEvents: [],
			certVerified: false,
			domainName: null,
			caCert: null,
			dashboardLoginToken: null,
			prompt: null,
			deleting: false,
			failed: false,
			errorDismissed: false
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
			certVerified: prevState.certVerified,

			domainName: attrs.domain ? attrs.domain.domain : prevState.domainName,
			caCert: attrs.ca_cert ? window.btoa(attrs.ca_cert) : prevState.caCert,
			dashboardLoginToken: attrs.dashboard_login_token || prevState.dashboardLoginToken,

			prompt: prevState.prompt
		};

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

	__setInstallState: function (newState) {
		this.__installState.setState(newState);
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
		}
	},

	__addLog: function (data) {
		var logEvents = [].concat(this.getInstallState().logEvents);
		logEvents.push(data);
		this.__setInstallState({
			logEvents: logEvents
		});
	}
});

Cluster.jsonFields = {
	id: 'ID',
	state: 'state',
	region: 'region',
	num_instances: 'numInstances',
	instance_type: 'instanceType',
	controller_key: 'controllerKey',
	controller_pin: 'controllerPin',
	dashboard_login_token: 'dashboardLoginToken',
	domain: 'domain',
	ca_cert: 'caCert',
	vpc_cidr: 'vpcCidr',
	subnetCidr: 'subnetCidr',
	creds: 'creds'
};

export default Cluster;
