//= require ../store
//= require ./app-jobs

(function () {

"use strict";

var TaffyJobs = Dashboard.Stores.TaffyJobs = Dashboard.Store.createClass({
	displayName: "Stores.TaffyJobs",

	getStateForApp: function (appId) {
		var state = {};
		state.processes = this.state.processes.filter(function (process) {
			return process.meta && process.meta.app === appId;
		});
		return state;
	},

	willInitialize: function () {
		this.props = {
			appId: "taffy"
		};
	}
});

TaffyJobs.prototype.handleEvent      = Dashboard.Stores.AppJobs.prototype.handleEvent;
TaffyJobs.prototype.__fetchJobs      = Dashboard.Stores.AppJobs.prototype.__fetchJobs;
TaffyJobs.prototype.__watchAppJobs   = Dashboard.Stores.AppJobs.prototype.__watchAppJobs;
TaffyJobs.prototype.__unwatchAppJobs = Dashboard.Stores.AppJobs.prototype.__unwatchAppJobs;
TaffyJobs.prototype.__getClient      = Dashboard.Stores.AppJobs.prototype.__getClient;
TaffyJobs.prototype.getInitialState  = Dashboard.Stores.AppJobs.prototype.getInitialState;
TaffyJobs.prototype.didInitialize    = Dashboard.Stores.AppJobs.prototype.didInitialize;
TaffyJobs.prototype.didBecomeActive  = Dashboard.Stores.AppJobs.prototype.didBecomeActive;

TaffyJobs.registerWithDispatcher(Dashboard.Dispatcher);

})();
