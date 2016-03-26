import { assertEqual, extend } from 'marbles/utils';
import Modal from 'Modal';
import GithubRepoStore from '../stores/github-repo';
import GithubCommitStore from '../stores/github-commit';
import GithubPullStore from '../stores/github-pull';
import BuildpackStore from '../stores/github-repo-buildpack';
import JobOutputStore from '../stores/job-output';
import { AppDeployStore } from 'dashboard/stores/app-deploy';
import RouteLink from './route-link';
import CommandOutput from './command-output';
import EditEnv from './edit-env';
import GithubCommit from './github-commit';
import GithubPull from './github-pull';
import ProviderPicker from 'dashboard/views/provider-picker';
import GithubRepoBuildpack from './github-repo-buildpack';
import Dispatcher from 'dashboard/dispatcher';

function getDeployStoreId (props) {
	return {
		appID: props.appID,
		sha: props.sha
	};
}

function getRepoStoreId (props) {
	return {
		ownerLogin: props.ownerLogin,
		name: props.repoName
	};
}

function getCommitStoreId (props) {
	return {
		ownerLogin: props.ownerLogin,
		repoName: props.repoName,
		sha: props.sha
	};
}

function getPullStoreId (props) {
	return {
		ownerLogin: props.baseOwner,
		repoName: props.baseRepo,
		number: props.pullNumber
	};
}

function getBuildpackStoreId (props, commit, pull) {
	return {
		ownerLogin: props.ownerLogin,
		repoName: props.repoName,
		ref: commit ? commit.sha : (pull ? pull.head.sha : props.branchName)
	};
}

function getState (props, prevState, providerIDs) {
	prevState = prevState || {};
	var state = {
		launching: prevState.launching,
		deleting: prevState.deleting,
		env: prevState.env || {},
		name: prevState.name,
		providerIDs: providerIDs === undefined ? prevState.providerIDs : providerIDs
	};

	state.deployStoreId = getDeployStoreId(props);
	var deployState = AppDeployStore.getState(state.deployStoreId);
	state.launching = deployState.launching;
	state.launchSuccess = deployState.launchSuccess;
	state.launchFailed = deployState.launchFailed;
	state.launchErrorMsg = deployState.launchErrorMsg;
	if (deployState.name !== null) {
		state.name = deployState.name;
	}
	if (deployState.release !== null) {
		state.env = deployState.release.env || {};
	}

	state.jobOutputStoreId = null;
	if (deployState.taffyJob !== null) {
		state.jobOutputStoreId = {
			appId: 'taffy',
			jobId: deployState.taffyJob.id,
			lines: 10000 // show full backlog
		};
	}
	var prevJobOutputStoreId = prevState.jobOutputStoreId;
	var nextJobOutputStoreId = state.jobOutputStoreId;
	if ( !assertEqual(prevJobOutputStoreId, nextJobOutputStoreId) ) {
		if (prevJobOutputStoreId) {
			JobOutputStore.removeChangeListener(prevJobOutputStoreId, this.__handleStoreChange);
		}
		if (nextJobOutputStoreId !== null) {
			JobOutputStore.addChangeListener(nextJobOutputStoreId, this.__handleStoreChange);
		}
	}

	var jobOutputState;
	if (state.jobOutputStoreId !== null) {
		jobOutputState = JobOutputStore.getState(state.jobOutputStoreId);
		state.jobOutput = jobOutputState.output;
		state.jobError = jobOutputState.streamError;
	}

	if (props.pullNumber) {
		state.pullStoreId = getPullStoreId(props);
		state.pull = GithubPullStore.getState(state.pullStoreId).pull;
	} else {
		state.commitStoreId = getCommitStoreId(props);
		state.commit = GithubCommitStore.getState(state.commitStoreId).commit;
	}

	var prevBuildpackStoreId = prevState.buildpackStoreId;
	var nextBuildpackStoreId = getBuildpackStoreId(props, state.commit, state.pull);
	if ( !assertEqual(prevBuildpackStoreId, nextBuildpackStoreId) ) {
		BuildpackStore.removeChangeListener(prevBuildpackStoreId, this.__handleStoreChange);
		BuildpackStore.addChangeListener(nextBuildpackStoreId, this.__handleStoreChange);
	}
	state.buildpackStoreId = nextBuildpackStoreId;
	state.buildpack = BuildpackStore.getState(state.buildpackStoreId);
	if (state.buildpack.unknown && !state.env.BUILDPACK_URL) {
		state.env.BUILDPACK_URL = "";
	}

	state.repoStoreId = getRepoStoreId(props);
	state.repo = GithubRepoStore.getState(state.repoStoreId).repo;

	state.launchDisabled = props.launchDisabled || !!(!state.repo || !(state.commit || state.pull) || state.launching || state.deleting);

	return state;
}

var GithubDeploy = React.createClass({
	displayName: "Views.GithubDeploy",

	render: function () {
		var commit = this.state.commit;
		var pull = this.state.pull;
		return (
			<Modal visible={true} onHide={this.props.onHide} className="github-deploy">
				<header>
					<h1>Launch app</h1>
					<h2>
						{this.props.ownerLogin +"/"+ this.props.repoName +":"+ this.props.branchName}
					</h2>
					{commit ? (
						<GithubCommit commit={commit} />
					) : (pull ? (
						<GithubPull pull={pull} />
					) : null)}

					<GithubRepoBuildpack
						ownerLogin={this.props.ownerLogin}
						repoName={this.props.repoName}
						selectedBranchName={this.state.buildpackStoreId.ref} />
				</header>

				{this.props.children}

				{this.state.launching ? null : (
					<div>
						<label>
							<span className="name">Name</span>
							<input type="text" value={this.state.name} onChange={this.__handleNameChange} />
						</label>

						{this.state.launching || this.state.deleting || this.state.launchSuccess || this.state.launchFailed ? null : (
							<ProviderPicker
								selected={this.state.providers}
								onChange={this.__handleProvidersChange} />
						)}

						<EditEnv
							disabled={this.state.launching || this.state.deleting || this.state.launchSuccess || this.state.launchFailed}
							env={this.state.env}
							onChange={this.__handleEnvChange} />
					</div>
				)}

				{this.state.jobOutput ? (
					<CommandOutput outputStreamData={this.state.jobOutput} showTimestamp={false} />
				) : null}

				{this.props.errorMsg ? (
					<div className="alert-error">{this.props.errorMsg}</div>
				) : null}

				{this.state.launchErrorMsg !== null ? (
					<div className="alert-error">{this.state.launchErrorMsg}</div>
				) : null}

				{this.state.launchSuccess === true ? (
					<RouteLink className="launch-btn" path={this.props.getAppPath()}>Continue</RouteLink>
				) : (
					(this.state.launchFailed === true ? (
						<button className="delete-btn" disabled={this.state.launchDisabled} onClick={this.__handleDeleteBtnClick}>{this.state.deleting ? "Deleting..." : "Launch failed. Delete app"}</button>
					) : (
						<button className="launch-btn" disabled={this.state.launchDisabled} onClick={this.__handleLaunchBtnClick}>{this.state.launching ? "Launching..." : "Launch app"}</button>
					))
				)}
			</Modal>
		);
	},

	getInitialState: function () {
		return getState.call(this, this.props, {
			name: this.__formatName([this.props.ownerLogin, this.props.repoName, this.props.branchName].join("-")),
			db: false,
			env: {}
		});
	},

	componentDidMount: function () {
		AppDeployStore.addChangeListener(this.state.deployStoreId, this.__handleStoreChange);
		GithubRepoStore.addChangeListener(this.state.repoStoreId, this.__handleStoreChange);
		if (this.state.commitStoreId) {
			GithubCommitStore.addChangeListener(this.state.commitStoreId, this.__handleStoreChange);
		}
		if (this.state.pullStoreId) {
			GithubPullStore.addChangeListener(this.state.pullStoreId, this.__handleStoreChange);
		}
		if (this.state.buildpackStoreId) {
			BuildpackStore.addChangeListener(this.state.buildpackStoreId, this.__handleStoreChange);
		}
		if (this.state.jobOutputStoreId !== null) {
			JobOutputStore.addChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
		}
	},

	componentWillReceiveProps: function (props) {
		var didChange = false;

		if (props.errorMsg) {
			didChange = true;
		}

		var prevDeployStoreId = this.state.deployStoreId;
		var nextDeployStoreId = getDeployStoreId(props);
		if ( !assertEqual(prevDeployStoreId, nextDeployStoreId) ) {
			AppDeployStore.removeChangeListener(prevDeployStoreId, this.__handleStoreChange);
			AppDeployStore.addChangeListener(nextDeployStoreId, this.__handleStoreChange);
			didChange = true;
		}

		var prevRepoStoreId = this.state.repoStoreId;
		var nextRepoStoreId = getRepoStoreId(props);
		if ( !assertEqual(prevRepoStoreId, nextRepoStoreId) ) {
			GithubRepoStore.addChangeListener(prevRepoStoreId, this.__handleStoreChange);
			GithubRepoStore.removeChangeListener(nextRepoStoreId, this.__handleStoreChange);
			didChange = true;
		}

		if (this.state.commitStoreId) {
			var prevCommitStoreId = this.state.commitStoreId;
			var nextCommitStoreId = getCommitStoreId(props);
			if ( !assertEqual(prevCommitStoreId, nextCommitStoreId) ) {
				GithubCommitStore.removeChangeListener(prevCommitStoreId, this.__handleStoreChange);
				GithubCommitStore.addChangeListener(nextCommitStoreId, this.__handleStoreChange);
				didChange = true;
			}
		}

		if (this.state.pullStoreId) {
			var prevPullStoreId = this.state.pullStoreId;
			var nextPullStoreId = getPullStoreId(props);
			if ( !assertEqual(prevPullStoreId, nextPullStoreId) ) {
				GithubPullStore.removeChangeListener(prevPullStoreId, this.__handleStoreChange);
				GithubPullStore.addChangeListener(nextPullStoreId, this.__handleStoreChange);
				didChange = true;
			}
		}

		if (didChange) {
			this.__handleStoreChange(props);
		}
	},

	componentWillUnmount: function () {
		AppDeployStore.removeChangeListener(this.state.deployStoreId, this.__handleStoreChange);
		GithubRepoStore.removeChangeListener(this.state.repoStoreId, this.__handleStoreChange);
		if (this.state.commitStoreId) {
			GithubCommitStore.removeChangeListener(this.state.commitStoreId, this.__handleStoreChange);
		}
		if (this.state.pullStoreId) {
			GithubPullStore.removeChangeListener(this.state.pullStoreId, this.__handleStoreChange);
		}
		if (this.state.buildpackStoreId) {
			BuildpackStore.removeChangeListener(this.state.buildpackStoreId, this.__handleStoreChange);
		}
		if (this.state.jobOutputStoreId !== null) {
			JobOutputStore.removeChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
		}
	},

	__handleStoreChange: function (props, providerIDs) {
		if (this.isMounted()) {
			this.setState(getState.call(this, props || this.props, this.state, providerIDs));
		}
	},

	__handleNameChange: function (e) {
		var name = e.target.value;
		this.setState({
			name: this.__formatName(name)
		});
	},

	__handleProvidersChange: function (providerIDs) {
		this.__handleStoreChange(this.props, providerIDs);
	},

	__handleEnvChange: function (env) {
		this.setState({
			env: env
		});
	},

	__handleDeleteBtnClick: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'DELETE_APP',
			appID: this.props.appID
		});
		this.setState({
			deleting: true,
			launchDisabled: true
		});
	},

	__handleLaunchBtnClick: function (e) {
		e.preventDefault();
		var appData = extend({
			name: this.state.name,
			providerIDs: this.state.providerIDs,
			env: this.state.env
		}, this.props.appData || {});
		if (this.props.errorMsg) {
			this.props.dismissError();
		}
		this.setState({
			launching: true,
			launchDisabled: true
		});
		if (this.state.pull) {
			Dispatcher.dispatch({
				name: 'DEPLOY_APP',
				source: 'GH_PULL',
				repo: this.state.repo,
				pull: this.state.pull,
				appData: appData
			});
		} else {
			Dispatcher.dispatch({
				name: 'DEPLOY_APP',
				source: 'GH_COMMIT',
				repo: this.state.repo,
				branchName: this.props.branchName,
				commit: this.state.commit,
				appData: appData
			});
		}
	},

	__formatName: function (name) {
		name = name.replace(/[^-a-z\d]/gi, '').replace(/^[^a-z\d]/i, '');
		name = name.toLowerCase().substr(0, 30);
		return name;
	}
});

export default GithubDeploy;
