import { assertEqual, extend } from 'marbles/utils';
import Modal from 'Modal';
import GithubCommitStore from '../stores/github-commit';
import GithubRepoStore from '../stores/github-repo';
import JobOutputStore from '../stores/job-output';
import AppDeployStore from 'dashboard/stores/app-deploy';
import GithubCommit from './github-commit';
import CommandOutput from './command-output';
import Dispatcher from 'dashboard/dispatcher';

function getDeployStoreId (props) {
	return {
		appID: props.appId,
		sha: props.sha
	};
}

function getCommitStoreId (props) {
	return {
		ownerLogin: props.ownerLogin,
		repoName: props.repoName,
		sha: props.sha
	};
}

function getRepoStoreId (props) {
	return {
		ownerLogin: props.ownerLogin,
		name: props.repoName
	};
}

function getState (props, prevState) {
	prevState = prevState || {};
	var state = {
		launching: prevState.launching || false
	};

	state.deployStoreId = getDeployStoreId(props);
	var deployState = AppDeployStore.getState(state.deployStoreId);
	state.launching = deployState.launching === false ? false : prevState.launching;
	state.launchSuccess = deployState.launchSuccess;
	state.launchFailed = deployState.launchFailed;
	state.launchErrorMsg = deployState.launchErrorMsg;

	state.commitStoreId = getCommitStoreId(props);
	state.commit = GithubCommitStore.getState(state.commitStoreId).commit;

	state.repoStoreId = getRepoStoreId(props);
	state.repo = GithubRepoStore.getState(state.repoStoreId).repo;

	state.jobOutputStoreId = null;
	if (deployState.taffyJob !== null) {
		state.jobOutputStoreId = {
			appId: 'taffy',
			jobId: deployState.taffyJob.id
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

	state.launchDisabled = !state.commit || !state.repo || state.launching;

	return state;
}

var AppDeployCommit = React.createClass({
	displayName: "Views.AppDeployCommit",

	render: function () {
		var commit = this.state.commit;

		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-deploy">
					<header>
						<h1>Deploy commit?</h1>
					</header>

					{commit ? (
						<GithubCommit commit={commit} />
					) : null}

					{this.state.jobOutput ? (
						<CommandOutput outputStreamData={this.state.jobOutput} showTimestamp={false} />
					) : null}

					{this.state.jobError ? (
						<div className="alert-error">{this.state.jobError}</div>
					) : null}

					{this.state.launchSuccess === true ? (
						<button className="deploy-btn" onClick={this.__handleDismissBtnClick}>Continue</button>
					) : (
						<button className="deploy-btn" disabled={this.state.launchDisabled} onClick={this.__handleDeployBtnClick}>{this.state.launching ? "Deploying..." : "Deploy"}</button>
					)}
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return extend(getState.call(this, this.props));
	},

	componentDidMount: function () {
		AppDeployStore.addChangeListener(this.state.deployStoreId, this.__handleStoreChange);
		GithubCommitStore.addChangeListener(this.state.commitStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var didChange = false;

		if (props.errorMsg) {
			didChange = true;
		}

		var prevCommitStoreId = this.state.commitStoreId;
		var nextCommitStoreId = getCommitStoreId(props);
		if ( !assertEqual(prevCommitStoreId, nextCommitStoreId) ) {
			GithubCommitStore.removeChangeListener(prevCommitStoreId, this.__handleStoreChange);
			GithubCommitStore.addChangeListener(nextCommitStoreId, this.__handleStoreChange);
			didChange = true;
		}

		var prevRepoStoreId = this.state.commitStoreId;
		var nextRepoStoreId = getRepoStoreId(props);
		if ( !assertEqual(prevRepoStoreId, nextRepoStoreId) ) {
			GithubRepoStore.removeChangeListener(prevRepoStoreId, this.__handleStoreChange);
			GithubRepoStore.addChangeListener(nextRepoStoreId, this.__handleStoreChange);
			didChange = true;
		}

		if (didChange) {
			this.__handleStoreChange(props);
		}
	},

	componentWillUnmount: function () {
		AppDeployStore.removeChangeListener(this.state.deployStoreId, this.__handleStoreChange);
		GithubCommitStore.removeChangeListener(this.state.commitStoreId, this.__handleStoreChange);
		GithubRepoStore.removeChangeListener(this.state.repoStoreId, this.__handleStoreChange);
		if (this.state.jobOutputStoreId !== null) {
			JobOutputStore.removeChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
		}
	},

	__handleStoreChange: function (props) {
		if (this.isMounted()) {
			this.setState(getState.call(this, props || this.props, this.state));
		}
	},

	__handleDeployBtnClick: function (e) {
		e.preventDefault();
		this.setState({
			launching: true,
			launchDisabled: true
		});
		Dispatcher.dispatch({
			name: 'APP_DEPLOY_COMMIT',
			appID: this.props.appId,
			ownerLogin: this.props.ownerLogin,
			repoName: this.props.repoName,
			branchName: this.props.branchName,
			repo: this.state.repo,
			sha: this.props.sha
		});
	},

	__handleDismissBtnClick: function (e) {
		e.preventDefault();
		this.props.onHide();
	}
});

export default AppDeployCommit;
