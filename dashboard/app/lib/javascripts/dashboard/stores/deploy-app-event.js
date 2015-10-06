import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';
import { assertEqual } from 'marbles/utils';

var DeployAppEvent = Store.createClass({
	displayName: "Stores.DeployAppEvent",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = this.id;
	},

	getInitialState: function () {
		return {
			isScale: false,
			isRelease: false,
			event: null,
			release: null,
			prevRelease: null,
			envDiff: null,
			processes: null,
			deploying: false,
			deploySuccess: null,
			deployErrorMsg: null
		};
	},

	didBecomeActive: function () {
		this.__fetchEvent();
	},

	didBecomeInactive: function () {
		this.constructor.discardInstance(this);
	},

	handleEvent: function (event) {
		var state = this.state;
		if (event.name === 'DEPLOYMENT' && state.event && state.event.object_type === 'app_release' && event.app === state.event.app && event.data.release === state.event.object_id) {
			this.__handleDeploymentEvent(event);
			return;
		}
		if (event.name === 'SCALE' && state.event && state.event.object_type === 'scale' && event.app === state.event.app) {
			if (assertEqual(state.processes, event.data.processes)) {
				this.setState({
					deploying: false,
					deploySuccess: true,
					deployErrorMsg: null
				});
			} else {
				this.setState({
					deploying: false,
					deploySuccess: false,
					deployErrorMsg: 'Something went wrong'
				});
			}
			return;
		}
		switch (event.name) {
		case 'APP_DEPLOY_RELEASE':
			if (state.event && state.event.object_type === 'app_release' && event.releaseID === state.event.object_id) {
				this.setState({
					deploying: true,
					deploySuccess: null,
					deployErrorMsg: null
				});
			}
			break;

		case 'APP_PROCESSES:CREATE_FORMATION':
			if (state.event && state.event.object_type === 'scale' && assertEqual(event.formation.processes, state.processes)) {
				this.setState({
					deploying: true,
					deploySuccess: null,
					deployErrorMsg: null
				});
			}
			break;
		}
	},

	__handleDeploymentEvent: function (event) {
		if (event.data.status === 'failed') {
			this.setState({
				deploying: false,
				deploySuccess: false,
				deployErrorMsg: event.data.error || 'Something went wrong'
			});
		} else if (event.data.status === 'complete') {
			this.setState({
				deploying: false,
				deploySuccess: true,
				deployErrorMsg: null
			});
		}
	},

	__fetchEvent: function () {
		Config.client.getEvent(this.props.eventID).then(function (args) {
			var event = args[0];
			var release = event.data.release || null;
			var isScale = false;
			var isRelease = false;
			if (event.object_type === 'app_release') {
				isRelease = true;
			} else if (event.object_type === 'scale') {
				isScale = true;
			}
			var processes = event.data.processes || null;
			this.setState({
				isScale: isScale,
				isRelease: isRelease,
				event: event,
				release: isRelease ? release : null,
				processes: isScale ? processes : null
			});
		}.bind(this));
	}
});

DeployAppEvent.isValidId = function (id) {
	return !!id.eventID;
};

DeployAppEvent.dispatcherIndex = DeployAppEvent.registerWithDispatcher(Dispatcher);

export default DeployAppEvent;
