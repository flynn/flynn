import Store from '../store';
import Dispatcher from '../dispatcher';
import AppJobs from './app-jobs';

var TaffyJobs = Store.createClass({
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

TaffyJobs.prototype.handleEvent      = AppJobs.prototype.handleEvent;
TaffyJobs.prototype.__fetchJobs      = AppJobs.prototype.__fetchJobs;
TaffyJobs.prototype.__watchAppJobs   = AppJobs.prototype.__watchAppJobs;
TaffyJobs.prototype.__unwatchAppJobs = AppJobs.prototype.__unwatchAppJobs;
TaffyJobs.prototype.__getClient      = AppJobs.prototype.__getClient;
TaffyJobs.prototype.getInitialState  = AppJobs.prototype.getInitialState;
TaffyJobs.prototype.didInitialize    = AppJobs.prototype.didInitialize;
TaffyJobs.prototype.didBecomeActive  = AppJobs.prototype.didBecomeActive;

TaffyJobs.registerWithDispatcher(Dispatcher);

export default TaffyJobs;
