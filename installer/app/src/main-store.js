import { extend, createClass } from 'marbles/utils';
import State from 'marbles/state';
import Client from './client';

export default createClass({
	mixins: [State],

	registerWithDispatcher: function (dispatcher) {
		this.dispatcherIndex = dispatcher.register(this.handleEvent.bind(this));
	},

	willInitialize: function () {
		this.state = this.getInitialState();
		this.__changeListeners = [];
	},

	getInitialState: function () {
		return {
			installEvents: [],
			installID: null,
			domain: null,
			dashboardLoginToken: null,
			cert: null,
			certVerified: false,

			steps: [
				{ id: 'configure', label: 'Configure' },
				{ id: 'install', label: 'Install' },
				{ id: 'dashboard', label: 'Dashboard' }
			],
			currentStep: 'configure',
			completedSteps: [],

			prompt: null
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
			case 'LOAD_INSTALL':
				if (this.state.installID !== event.id) {
					this.setState({
						installID: event.id,
						completedSteps: ['configure'],
						currentStep: 'install'
					});
					Client.closeEventStream();
					Client.openEventStream(event.id);
				}
				Client.checkInstallExists(event.id);
			break;

			case 'INSTALL_EXISTS':
				if ( !event.exists && (!this.state.installID || event.id === this.state.installID) ) {
					Client.closeEventStream();
					this.setState(this.getInitialState());
				}
			break;

			case 'LAUNCH_AWS':
				this.launchAWS(event);
			break;

			case 'LAUNCH_INSTALL_SUCCESS':
				this.setState({
					installID: event.res.id
				});
				Client.closeEventStream();
				Client.openEventStream(event.res.id);
			break;

			case 'LAUNCH_INSTALL_FAILURE':
				window.console.error(event);
			break;

			case 'INSTALL_PROMPT_REQUESTED':
				this.setState({
					prompt: event.data
				});
			break;

			case 'INSTALL_PROMPT_RESOLVED':
				if (this.state.prompt && this.state.prompt.id === event.data.id) {
					this.setState({
						prompt: null
					});
				}
			break;

			case 'INSTALL_PROMPT_RESPONSE':
				Client.sendPromptResponse(event.data.id, event.data);
			break;

			case 'DOMAIN':
				this.setState({
					domain: event.domain
				});
			break;

			case 'DASHBOARD_LOGIN_TOKEN':
				this.setState({
					dashboardLoginToken: event.token
				});
			break;

			case 'CA_CERT':
				this.setState({
					cert: event.cert
				});
			break;

			case 'CHECK_CERT':
				Client.checkCert(this.state.domain);
			break;

			case 'CERT_VERIFIED':
				this.setState({
					certVerified: true
				});
			break;

			case 'INSTALL_EVENT':
				this.handleInstallEvent(event.data);
			break;

			case 'INSTALL_DONE':
				if (this.state.cert && this.state.domain && this.state.dashboardLoginToken) {
					this.setState({
						completedSteps: ['configure', 'install'],
						currentStep: 'dashboard'
					});
					Client.checkCert(this.state.domain);
				}
			break;
		}
	},

	handleInstallEvent: function (data) {
		this.setState({
			installEvents: this.state.installEvents.concat([data])
		});
	},

	launchAWS: function (inputs) {
		var data = {
			creds: inputs.cred,
			region: inputs.region,
			instanceType: inputs.instanceType,
			numInstances: inputs.numInstances
		};

		if (inputs.vpcCidr) {
			data.vpc_cidr = inputs.vpcCidr;
		}

		if (inputs.subnetCidr) {
			data.subnet_cidr = inputs.subnetCidr;
		}

		this.setState(extend({}, this.getInitialState(), {
			completedSteps: ['configure'],
			currentStep: 'install'
		}));
		Client.launchInstall(data);
	}
});
