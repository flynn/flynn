//= require ../store
//= require ../dispatcher

(function () {

"use strict";

var JobOutput = Dashboard.Stores.JobOutput = Dashboard.Store.createClass({
	displayName: "Stores.JobOutput",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = {
			appId: this.id.appId,
			jobId: this.id.jobId
		};
		this.url = Dashboard.config.endpoints.cluster_controller + "/apps/"+ this.props.appId +"/jobs/"+ this.props.jobId +"/log?tail=true&key="+ encodeURIComponent(Dashboard.config.user.controller_key);
	},

	didBecomeActive: function () {
		this.__openEventStream();
	},

	didBecomeInactive: function () {
		return this.constructor.__super__.didBecomeInactive.apply(this, arguments).then(function () {
			if (this.__eventSource) {
				this.__eventSource.close();
				this.setState({
					open: false
				});
			}
		}.bind(this));
	},

	getInitialState: function () {
		return {
			open: false,
			eof: false,
			output: [],
			streamError: null
		};
	},

	handleEvent: function () {
	},

	__openEventStream: function (retryCount) {
		if ( !window.hasOwnProperty('EventSource') ) {
			return;
		}

		this.setState(Marbles.Utils.extend(this.getInitialState(), {open: true}));

		var url = this.url;
		var eventSource;
		var open = false;
		eventSource = new window.EventSource(url, {withCredentials: true});
		eventSource.addEventListener("error", function () {
			eventSource.close();
			if ( !open && (!retryCount || retryCount < 3) ) {
				setTimeout(function () {
					this.__openEventStream((retryCount || 0) + 1);
				}.bind(this), 300);
			} else {
				this.setState({
					open: false,
					eof: false,
					streamError: "Failed to connect to log"
				});
			}
		}.bind(this), false);
		eventSource.addEventListener("open", function () {
			open = true;
		});
		eventSource.addEventListener("message", function (e) {
			this.setState({
				output: this.state.output.concat(JSON.parse(e.data))
			});
		}.bind(this), false);
		eventSource.addEventListener("eof", function () {
			this.setState({
				open: false,
				eof: true
			});
			eventSource.close();
		}.bind(this), false);
		eventSource.addEventListener("exit", function (e) {
			var data = JSON.parse(e.data);
			Dashboard.Dispatcher.handleStoreEvent({
				name: "JOB_EXIT",
				jobId: this.props.jobId,
				status: data.status
			});
			if (data.status === 0) {
				this.setState({
					open: false,
					eof: true
				});
			} else {
				this.setState({
					open: false,
					eof: false,
					streamError: "Non-zero exit status: "+ data.status
				});
			}
			eventSource.close();
		}.bind(this), false);

		this.__eventSource = eventSource;
	}

}, Marbles.State);

JobOutput.isValidId = function (id) {
	return id.appId && id.jobId;
};

JobOutput.registerWithDispatcher(Dashboard.Dispatcher);

})();
