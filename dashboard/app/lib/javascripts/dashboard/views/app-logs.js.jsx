import { assertEqual } from 'marbles/utils';
import Modal from 'Modal';
import AppJobsStore from '../stores/app-jobs';
import TaffyJobsStore from '../stores/taffy-jobs';
import JobOutput from './job-output';
import ExternalLink from './external-link';
import Timestamp from './timestamp';

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

	var taffyJobsState = TaffyJobsStore.getStateForApp(props.taffyJobsStoreId, props.appId);
	state.deployProcesses = taffyJobsState.processes;

	return state;
}

var AppLogs = React.createClass({
	displayName: "Views.AppLogs",

	render: function () {
		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-logs">
					<header>
						<h1>Process logs</h1>
					</header>

					{this.state.processes.length > 0 ? (
						<ul className="processes">
							{this.state.processes.map(function (process) {
								return (
									<li key={process.id} onClick={function () {
										this.__handleProcessSelected(process);
									}.bind(this)} className={this.state.selectedProcess && this.state.selectedProcess.id === process.id ? "selected" : null}>
										{process.type}
										<span className={"state "+ process.state}>{process.state}</span>
										<span className="float-right">
											<Timestamp timestamp={process.created_at} />
										</span>
									</li>
								);
							}, this)}
						</ul>
					) : (
						<p className="placeholder">There are no logs yet</p>
					)}

					<section className="log-output">
						{this.state.selectedProcess ? (
							<JobOutput
								appId={this.props.appId}
								jobId={this.state.selectedProcess.id}
								lines={this.props.lines} />
						) : null}
					</section>
				</section>

				<section className="app-logs">
					<header>
						<h1>Deploy logs</h1>
					</header>

					{this.state.deployProcesses.length > 0 ? (
						<ul className="processes">
							{this.state.deployProcesses.map(function (process) {
								return (
									<li key={process.id} onClick={function (e) {
										if (e.target.nodeName === "A") {
											return;
										}
										this.__handleDeployProcessSelected(process);
									}.bind(this)} className={this.state.selectedDeployProcess && this.state.selectedDeployProcess.id === process.id ? "selected" : null}>
										{this.__deployProcessNameComponent(process)}
										<span className={"state "+ process.state}>{this.__formatDeployProcessState(process.state)}</span>
										<span className="float-right">
											<Timestamp timestamp={process.created_at} />
										</span>
									</li>
								);
							}, this)}
						</ul>
					) : (
						<p className="placeholder">There are no logs yet</p>
					)}

					<section className="log-output">
						{this.state.selectedDeployProcess ? (
							<JobOutput
								appId={"taffy"}
								jobId={this.state.selectedDeployProcess.id}
								lines={this.props.lines} />
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
		TaffyJobsStore.addChangeListener(this.props.taffyJobsStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppJobsStoreId = this.state.appJobsStoreId;
		var nextAppJobsStoreId = getAppJobsStoreId(nextProps);
		if ( !assertEqual(prevAppJobsStoreId, nextAppJobsStoreId) ) {
			AppJobsStore.removeChangeListener(prevAppJobsStoreId, this.__handleStoreChange);
			AppJobsStore.addChangeListener(nextAppJobsStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		AppJobsStore.removeChangeListener(this.state.appJobsStoreId, this.__handleStoreChange);
		TaffyJobsStore.removeChangeListener(this.props.taffyJobsStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props));
	},

	__handleProcessSelected: function (process) {
		this.setState({
			selectedProcess: process
		});
	},

	__handleDeployProcessSelected: function (process) {
		this.setState({
			selectedDeployProcess: process
		});
	},

	__formatDeployProcessState: function (state) {
		switch (state) {
		case "up":
			return "running";
		case "down":
			return "finished";
		default:
			return state;
		}
	},

	__deployProcessNameComponent: function (process) {
		var meta = process.meta;
		if (meta.github !== "true") {
			return null;
		}
		return (
			<ExternalLink href={"https://github.com/"+ encodeURIComponent(meta.github_user) +"/"+ encodeURIComponent(meta.github_repo) +"/tree/"+ meta.rev}>
				{meta.rev.slice(0, 7)}
			</ExternalLink>
		);
	}
});

export default AppLogs;
