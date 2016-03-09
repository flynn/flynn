import { extend } from 'marbles/utils';
import { objectDiff } from 'dashboard/utils';
import DeployAppEventStore from 'dashboard/stores/deploy-app-event';
import GithubCommitStore from '../stores/github-commit';
import AppStore from '../stores/app';
import Dispatcher from 'dashboard/dispatcher';
import Modal from 'Modal';
import GithubCommit from './github-commit';
import ReleaseEvent from './release-event';
import ScaleEvent from './scale-event';

var deployAppEventStoreID = function (props) {
	return {
		eventID: props.eventID
	};
};

var appStoreID = function (props) {
	return {
		appId: props.appID
	};
};

var AppDeployEvent = React.createClass({
	render: function () {
		var state = this.state;
		var isRelease = state.isRelease;
		var isScale = state.isScale;
		var commit = state.commit;
		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-deploy">
					<header>
						{isRelease ? (
							<h1>Deploy release?</h1>
						) : (
							isScale ? (
								<h1>Deploy formation?</h1>
							) : (
								<h1>&nbsp;</h1>
							)
						)}
					</header>

					{isRelease ? (
						<ReleaseEvent
							release={state.release}
							envDiff={state.envDiff || []}
							style={{
								marginBottom: '1rem'
							}} />
					) : null}

					{isScale ? (
						<ScaleEvent
							prevProcesses={state.prevProcesses}
							delta={state.scaleDelta}
							diff={state.processDiff}
							style={{
								marginBottom: '1rem'
							}} />
					) : null}

					{commit !== null ? (
						<GithubCommit commit={commit} />
					) : null}

					{state.deployErrorMsg !== null ? (
						<div className='alert-error'>
							{state.deployErrorMsg}
						</div>
					) : null}

					{state.deploySuccess !== true ? (
						<button className="deploy-btn" disabled={state.deployDisabled} onClick={this.__handleDeployBtnClick}>{state.deploying ? "Deploying..." : "Deploy"}</button>
					) : null}
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		DeployAppEventStore.addChangeListener(deployAppEventStoreID(this.props), this.__handleStoreChange);
		AppStore.addChangeListener(appStoreID(this.props), this.__handleStoreChange);
	},

	componentWillUpdate: function (nextProps, nextState) {
		var prevState = this.state;
		if (prevState.githubCommitStoreID === null && nextState.githubCommitStoreID !== null) {
			GithubCommitStore.addChangeListener(nextState.githubCommitStoreID, this.__handleStoreChange);
		}
	},

	componentWillUnmount: function () {
		DeployAppEventStore.removeChangeListener(deployAppEventStoreID(this.props), this.__handleStoreChange);
		AppStore.removeChangeListener(appStoreID(this.props), this.__handleStoreChange);
		if (this.state.githubCommitStoreID !== null) {
			GithubCommitStore.removeChangeListener(this.state.githubCommitStoreID, this.__handleStoreChange);
		}
	},

	__getState: function (props) {
		var state = extend({}, DeployAppEventStore.getState(deployAppEventStoreID(props)));
		state.appState = AppStore.getState(appStoreID(props));

		state.envDiff = null;
		if (state.appState.release !== null && state.event !== null && state.event.object_type === 'app_release') {
			state.envDiff = objectDiff(state.appState.release.env || {}, state.event.data.release.env || {});
		}

		var commitState = {};
		var meta = state.release !== null ? state.release.meta || null : null;
		if (meta !== null && meta.github === 'true') {
			state.githubCommitStoreID = {
				ownerLogin: meta.github_user,
				repoName: meta.github_repo,
				sha: meta.rev
			};
			commitState = GithubCommitStore.getState(state.githubCommitStoreID);
			state.commit = commitState.commit;
		} else {
			state.githubCommitStoreID = null;
			state.commit = null;
		}

		extend(state, this.__scaleEventState(props, state));

		state.deployDisabled = state.event === null || state.deploying || state.deployErrorMsg !== null;
		if (state.event && state.event.object_type === 'scale' && state.processDiff === null) {
			state.deployDisabled = true;
		}
		if (state.event && state.event.object_type === 'app_release' && state.envDiff === null) {
			state.deployDisabled = true;
		}
		return state;
	},

	__scaleEventState: function (props, state) {
		if (state.event === null || state.event.object_type !== 'scale') {
			return {
				processDiff: null,
				scaleDelta: null,
				prevProcesses: null
			};
		}

		var processes = state.event.data.processes || {};
		var formationProcesses = (state.appState.formation || {}).processes || {};
		var diff = objectDiff(formationProcesses, processes);
		var delta = 0;
		diff.forEach(function (d) {
			if (d.op === 'replace') {
				delta += processes[d.key] - formationProcesses[d.key];
			} else if (d.op === 'add') {
				delta += d.value;
			} else if (d.op === 'remove') {
				delta -= formationProcesses[d.key];
			}
		});

		return {
			processDiff: diff,
			scaleDelta: delta,
			prevProcesses: formationProcesses
		};
	},

	__handleStoreChange: function () {
		if (this.isMounted()) {
			this.setState(this.__getState(this.props));
		}
	},

	__handleDismissBtnClick: function (e) {
		e.preventDefault();
		this.props.onHide();
	},

	__handleDeployBtnClick: function (e) {
		e.preventDefault();
		var event = this.state.event;
		if (event.object_type === 'app_release') {
			Dispatcher.dispatch({
				name: 'APP_DEPLOY_RELEASE',
				appID: this.props.appID,
				releaseID: event.object_id,
				deployTimeout: this.state.appState.app.deploy_timeout
			});
		} else if (event.object_type === 'scale') {
			Dispatcher.dispatch({
				name: 'APP_PROCESSES:CREATE_FORMATION',
				storeId: {
					appId: this.props.appID
				},
				formation: {
					app: this.props.appID,
					release: this.state.appState.app.release,
					processes: this.state.processes
				}
			});
		}
	}
});

export default AppDeployEvent;
