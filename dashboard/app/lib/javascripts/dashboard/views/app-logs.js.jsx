/** @jsx React.DOM */
//= require ../stores/app-jobs
//= require ./job-output
//= require Modal

(function () {

"use strict";

var AppJobsStore = Dashboard.Stores.AppJobs;

var Modal = window.Modal;

function getAppJobsStoreId (props) {
	return {
		appId: props.appId
	};
}

function getState (props) {
	var state = {
		appJobsStoreId: getAppJobsStoreId(props)
	};

	var appJobsState = AppJobsStore.getState(state.appJobsStoreId);
	state.processes = appJobsState.processes;

	return state;
}

Dashboard.Views.AppLogs = React.createClass({
	displayName: "Views.AppLogs",

	render: function () {
		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-logs">
					<header>
						<h1>Process logs</h1>
					</header>

					<ul className="processes">
						{this.state.processes.map(function (process) {
							return (
								<li key={process.id} onClick={function () {
									this.__handleProcessSelected(process);
								}.bind(this)} className={this.state.selectedProcess === process ? "selected" : null}>
									{process.type}
									<span className={"state "+ process.state}>{process.state}</span>
								</li>
							);
						}, this)}
					</ul>

					<section className="log-output">
						{this.state.selectedProcess ? (
							<Dashboard.Views.JobOutput
								appId={this.props.appId}
								jobId={this.state.selectedProcess.id} />
						) : null}
					</section>
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		AppJobsStore.addChangeListener(this.state.appJobsStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppJobsStoreId = this.state.appJobsStoreId;
		var nextAppJobsStoreId = getAppJobsStoreId(nextProps);
		if ( !Marbles.Utils.assertEqual(prevAppJobsStoreId, nextAppJobsStoreId) ) {
			AppJobsStore.removeChangeListener(prevAppJobsStoreId, this.__handleStoreChange);
			AppJobsStore.addChangeListener(nextAppJobsStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		AppJobsStore.removeChangeListener(this.state.appJobsStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props));
	},

	__handleProcessSelected: function (process) {
		this.setState({
			selectedProcess: process
		});
	}
});

})();
