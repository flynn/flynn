import { extend } from 'marbles/utils';
import Store from '../store';
import JobsStream from './jobs-stream';
import Dispatcher from '../dispatcher';
import Config from '../config';

var AppJobs = Store.createClass({
	displayName: "Stores.AppJobs",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = {
			appId: this.id.appId
		};
	},

	didInitialize: function () {
		this.client = this.__getClient();
	},

	didBecomeActive: function () {
		this.__fetchJobs();
		this.__watchAppJobs();
	},

	didBecomeInactive: function () {
		this.__unwatchAppJobs();
	},

	getInitialState: function () {
		return {
			processes: []
		};
	},

	handleEvent: function (event) {
		if (event.name === "JOB_STATE_CHANGE") {
			var hasChanges = false;
			this.state.processes.forEach(function (process) {
				if (process.id === event.jobId) {
					hasChanges = true;
					process.state = event.state;
				}
			});
			if (hasChanges) {
				this.setState({
					processes: this.state.processes
				});
			} else {
				this.__fetchJobs();
			}
		}
	},

	__fetchJobs: function () {
		this.client.getAppJobs(this.props.appId).then(function (args) {
			var res = args[0];
			this.setState({
				processes: res.map(function (item) {
					if (item.hasOwnProperty("State")) {
						item.state = item.State;
					}
					var state = item.state;
					if (item.state === 'down' && item.exit_status !== 0) {
						state = 'crashed';
					} else if (item.state === 'down' && item.host_error) {
						state = 'failed';
					}
					return extend({}, item, { state: state });
				})
			});
		}.bind(this));
	},

	__watchAppJobs: function () {
		JobsStream.addChangeListener({ appId: this.props.appId }, this.__handleJobsStreamChange);
	},

	__unwatchAppJobs: function () {
		JobsStream.removeChangeListener({ appId: this.props.appId }, this.__handleJobsStreamChange);
	},

	// We don't care about change events,
	// but have a listener setup to regulate when
	// the jobs stream is open
	__handleJobsStreamChange: function () {},

	__getClient: function () {
		return Config.client;
	}

});

AppJobs.isValidId = function (id) {
	return !!id.appId;
};

AppJobs.registerWithDispatcher(Dispatcher);

export default AppJobs;
