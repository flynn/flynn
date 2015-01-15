//= require ../store

(function () {

"use strict";

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
	},

	getInitialState: function () {
		return {
			processes: []
		};
	},

	handleEvent: function (event) {
		if (event.name === "JOB_EXIT") {
			var hasChanges = false;
			this.state.processes.forEach(function (process) {
				if (process.id === event.jobId) {
					hasChanges = true;
					process.state = event.status === 0 ? "down" : "crashed";
				}
			});
			if (hasChanges) {
				this.setState({
					processes: this.state.processes
				});
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

	__getClient: function () {
		return Dashboard.client;
	}

});

AppJobs.isValidId = function (id) {
	return !!id.appId;
};

AppJobs.registerWithDispatcher(Dashboard.Dispatcher);

})();
