//= require ../stores/job-output
//= require ./command-output

(function () {

"use strict";

var JobOutputStore = Dashboard.Stores.JobOutput;

function getJobOutputStoreId (props) {
	return {
		appId: props.appId,
		jobId: props.jobId
	};
}

function getJobOutputState (props) {
	var state = {
		jobOutputStoreId: getJobOutputStoreId(props)
	};

	var jobOutputState = JobOutputStore.getState(state.jobOutputStoreId);
	state.output = jobOutputState.output;
	state.streamError = jobOutputState.streamError;

	return state;
}

Dashboard.Views.JobOutput = React.createClass({
	displayName: "Views.JobOutput",

	render: function () {
		return (
			<Dashboard.Views.CommandOutput
				outputStreamData={this.state.streamError ? [this.state.streamError] : this.state.output} />
		);
	},

	getInitialState: function () {
		return getJobOutputState(this.props);
	},

	componentDidMount: function () {
		JobOutputStore.addChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var oldJobOutputStoreId = this.state.jobOutputStoreId;
		var newJobOutputStoreId = getJobOutputStoreId(props);
		if ( !Marbles.Utils.assertEqual(oldJobOutputStoreId, newJobOutputStoreId) ) {
			JobOutputStore.removeChangeListener(oldJobOutputStoreId, this.__handleStoreChange);
			JobOutputStore.addChangeListener(newJobOutputStoreId, this.__handleStoreChange);
		}
	},

	componentWillUnmount: function () {
		JobOutputStore.removeChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(getJobOutputState(this.props));
	}
});

})();
