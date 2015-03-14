//= require ../store
//= require ./jobs-stream
//= require ../dispatcher

(function () {

"use strict";

var JobsStream = Dashboard.Stores.JobsStream;

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
		this.url = Dashboard.config.endpoints.cluster_controller + "/apps/"+ this.props.appId +"/log?job_id="+ this.props.jobId +"&follow=true&key="+ encodeURIComponent(Dashboard.config.user.controller_key);
	},

	didBecomeActive: function () {
		this.__openEventStream();
		this.__watchAppJobs();
	},

	didBecomeInactive: function () {
		return this.constructor.__super__.didBecomeInactive.apply(this, arguments).then(function () {
			if (this.__eventSource) {
				this.__eventSource.close();
				this.setState({
					open: false
				});
				this.__unwatchAppJobs();
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

	setStateWithDelay: function (newState) {
		this.willUpdate();
		var state = this.state;
		Object.keys(newState).forEach(function (key) {
			state[key] = newState[key];
		});
		this.handleChangeWithDelay();
	},

	handleChangeWithDelay: function () {
		clearTimeout(this.__handleChangeTimeout);
		this.__handleChangeTimeout = setTimeout(function () {
			this.handleChangeDelayed();
		}.bind(this), 2);
		if ( !this.__handleChangeMaxTimeout ) {
			this.__handleChangeMaxTimeout = setTimeout(function () {
				this.handleChangeDelayed();
			}.bind(this), 10);
		}
	},

	handleChangeDelayed: function () {
		clearTimeout(this.__handleChangeMaxTimeout);
		clearTimeout(this.__handleChangeTimeout);
		this.handleChange();
		this.didUpdate();
	},

	handleEvent: function (event) {
		if (event.name === "JOB_STATE_CHANGE" && event.jobId === this.props.jobId) {
			if (event.state === "down") {
				this.setState({
					eof: true,
					open: false
				});
			} else if (event.state === "crashed") {
				this.setState({
					open: false,
					eof: false,
					streamError: "Non-zero exit status"
				});
			}
		}
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
		var handleError = function () {
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
		}.bind(this);
		eventSource.addEventListener("error", handleError, false);
		eventSource.addEventListener("open", function () {
			open = true;
		});
		eventSource.addEventListener("message", function (e) {
			var evnt = JSON.parse(e.data || "");
			switch (evnt.event) {
				case "error":
					handleError();
					return;
			}
			var data = evnt.data;
			if (data.msg && data.timestamp) {
				this.setStateWithDelay({
					output: this.state.output.concat([evnt.data])
				});
			}
		}.bind(this), false);

		this.__eventSource = eventSource;
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
	__handleJobsStreamChange: function () {}

});

JobOutput.isValidId = function (id) {
	return id.appId && id.jobId;
};

JobOutput.registerWithDispatcher(Dashboard.Dispatcher);

})();
