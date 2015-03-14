//= require ../store
//= require ./jobs-stream

(function () {

"use strict";

var JobsStream = Dashboard.Stores.JobsStream;

var AppJobs = Dashboard.Stores.AppJobs = Dashboard.Store.createClass({
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
					return item;
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
		return Dashboard.client;
	}

});

AppJobs.isValidId = function (id) {
	return !!id.appId;
};

AppJobs.registerWithDispatcher(Dashboard.Dispatcher);

})();
