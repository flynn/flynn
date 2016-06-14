import { assertEqual } from 'marbles/utils';
import AppStore from '../stores/app';
import AppJobsStore from '../stores/app-jobs';
import TaffyJobsStore from '../stores/taffy-jobs';
import JobOutput from './job-output';
import RouteLink from './route-link';
import ExternalLink from './external-link';
import Timestamp from './timestamp';

function getAppJobsStoreId (props) {
	return {
		appId: props.appId
	};
}

function getAppStoreId (props) {
	return {
		appId: props.appId
	};
}

function getState (props) {
	var state = {
		appJobsStoreId: getAppJobsStoreId(props),
		appStoreId: getAppStoreId(props)
	};

	var appJobsState = AppJobsStore.getState(state.appJobsStoreId);
	var processIDs = {};
	state.processes = appJobsState.processes.filter(function (p) {
		if (processIDs.hasOwnProperty(p.id)) {
			return false;
		}
		processIDs[p.id] = null;
		return p;
	});

	var taffyJobsState = TaffyJobsStore.getStateForApp(props.taffyJobsStoreId, props.appId);
	state.deployProcesses = taffyJobsState.processes;

	var appState = AppStore.getState(state.appStoreId);
	state.app = appState.app;

	return state;
}

var AppLogs = React.createClass({
	displayName: "Views.AppLogs",

	render: function () {
		return (
			<div className="app-logs panel-row full-height">
				<section className="panel full-height">
					<section className="app-back">
						<RouteLink path={'/apps/'+ encodeURIComponent(this.props.appId)}>« Back to {this.state.app ? this.state.app.name : 'app'}</RouteLink>
					</section>

					<section>
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
					</section>

					<section>
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
										}.bind(this)} className={this.state.selectedProcess && this.state.selectedProcess.id === process.id ? "selected" : null}>
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
					</section>
				</section>

				<section className="panel full-height log-output">
					{this.state.selectedProcess ? (
						<JobOutput
							key={this.state.selectedProcess.id}
							appId={this.state.selectedProcess.app}
							jobId={this.state.selectedProcess.id}
							lines={this.props.lines} />
					) : null}
				</section>
			</div>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		AppJobsStore.addChangeListener(this.state.appJobsStoreId, this.__handleStoreChange);
		TaffyJobsStore.addChangeListener(this.props.taffyJobsStoreId, this.__handleStoreChange);
		AppStore.addChangeListener(this.state.appStoreId, this.__handleStoreChange);
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
		AppStore.removeChangeListener(this.state.appStoreId, this.__handleStoreChange);
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
			selectedProcess: process
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
