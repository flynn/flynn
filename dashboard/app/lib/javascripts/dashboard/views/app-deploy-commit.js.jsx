//= require ../stores/github-commit
//= require ../stores/job-output
//= require ../actions/app-deploy-commit
//= require ./github-commit
//= require ./command-output
//= require Modal

(function () {

"use strict";

var GithubCommitStore = Dashboard.Stores.GithubCommit;
var JobOutputStore = Dashboard.Stores.JobOutput;

var AppDeployCommitActions = Dashboard.Actions.AppDeployCommit;

var Modal = window.Modal;

function getCommitStoreId (props) {
	return {
		ownerLogin: props.ownerLogin,
		repoName: props.repoName,
		sha: props.sha
	};
}

function getJobOutputStoreId (props) {
	if ( !props.job ) {
		return null;
	}
	return {
		appId: props.appId,
		jobId: props.job.id
	};
}

function getState (props, prevState) {
	prevState = prevState || {};
	var state = {
		deploying: prevState.deploying || false
	};

	state.commitStoreId = getCommitStoreId(props);
	state.commit = GithubCommitStore.getState(state.commitStoreId).commit;

	var jobOutputState;
	if (props.job) {
		state.jobOutputStoreId = getJobOutputStoreId(props);
		jobOutputState = JobOutputStore.getState(state.jobOutputStoreId);
		state.jobOutput = jobOutputState.output;
		state.jobError = jobOutputState.streamError;

		if (jobOutputState.open === false) {
			state.deploying = false;
		}

		if (jobOutputState.eof) {
			state.deployed = true;
		}
	}

	state.deployDisabled = !state.commit || state.deploying;

	if (props.errorMsg) {
		state.deployDisabled = false;
		state.deploying = false;
	}

	return state;
}

Dashboard.Views.AppDeployCommit = React.createClass({
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
						<Dashboard.Views.GithubCommit commit={commit} />
					) : null}

					{this.state.jobOutput ? (
						<Dashboard.Views.CommandOutput outputStreamData={this.state.jobOutput} />
					) : null}

					{this.props.errorMsg ? (
						<div className="alert-error">{this.props.errorMsg}</div>
					) : null}

					{this.state.jobError ? (
						<div className="alert-error">{this.state.jobError}</div>
					) : null}

					{this.state.deployed ? (
						<button className="deploy-btn" onClick={this.__handleDismissBtnClick}>Continue</button>
					) : (
						<button className="deploy-btn" disabled={this.state.deployDisabled} onClick={this.__handleDeployBtnClick}>{this.state.deploying ? "Deploying..." : "Deploy"}</button>
					)}
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return Marbles.Utils.extend(getState(this.props));
	},

	componentDidMount: function () {
		GithubCommitStore.addChangeListener(this.state.commitStoreId, this.__handleStoreChange);
		if (this.state.jobOutputStoreId) {
			JobOutputStore.addChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
		}
	},

	componentWillReceiveProps: function (props) {
		var didChange = false;

		if (props.errorMsg) {
			didChange = true;
		}

		var prevCommitStoreId = this.state.commitStoreId;
		var nextCommitStoreId = getCommitStoreId(props);
		if ( !Marbles.Utils.assertEqual(prevCommitStoreId, nextCommitStoreId) ) {
			GithubCommitStore.removeChangeListener(prevCommitStoreId, this.__handleStoreChange);
			GithubCommitStore.addChangeListener(nextCommitStoreId, this.__handleStoreChange);
			didChange = true;
		}
		var prevJobOutputStoreId = this.state.jobOutputStoreId;
		var nextJobOutputStoreId = getJobOutputStoreId(props);
		if ( !Marbles.Utils.assertEqual(prevJobOutputStoreId, nextJobOutputStoreId) ) {
			if (prevJobOutputStoreId) {
				JobOutputStore.removeChangeListener(prevJobOutputStoreId, this.__handleStoreChange);
			}
			if (nextJobOutputStoreId) {
				JobOutputStore.addChangeListener(nextJobOutputStoreId, this.__handleStoreChange);
			}
			didChange = true;
		}
		if (didChange) {
			this.__handleStoreChange(props);
		}
	},

	componentWillUnmount: function () {
		GithubCommitStore.removeChangeListener(this.state.commitStoreId, this.__handleStoreChange);
		if (this.state.jobOutputStoreId) {
			JobOutputStore.removeChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
		}
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props, this.state));
	},

	__handleDeployBtnClick: function (e) {
		e.preventDefault();
		this.setState({
			deploying: true,
			deployDisabled: true
		});
		AppDeployCommitActions.deployCommit(
			this.props.appId,
			this.props.ownerLogin,
			this.props.repoName,
			this.props.branchName,
			this.props.sha
		);
	},

	__handleDismissBtnClick: function (e) {
		e.preventDefault();
		this.props.onHide();
	}
});

})();
