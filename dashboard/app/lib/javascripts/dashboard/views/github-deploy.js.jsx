/** @jsx React.DOM */
//= require ../stores/github-commit
//= require ../stores/github-pull
//= require ../stores/app
//= require ../stores/job-output
//= require ../actions/github-deploy
//= require ./github-commit
//= require ./github-pull
//= require ./edit-env
//= require ./command-output
//= require ./route-link
//= require Modal

(function () {

"use strict";

var GithubRepoStore = Dashboard.Stores.GithubRepo;
var GithubCommitStore = Dashboard.Stores.GithubCommit;
var GithubPullStore = Dashboard.Stores.GithubPull;
var JobOutputStore = Dashboard.Stores.JobOutput;

var GithubDeployActions = Dashboard.Actions.GithubDeploy;

var RouteLink = Dashboard.Views.RouteLink;
var Modal = window.Modal;

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
		launching: prevState.launching
	};

	if (props.pullNumber) {
		state.pullStoreId = getPullStoreId(props);
		state.pull = GithubPullStore.getState(state.pullStoreId).pull;
	} else {
		state.commitStoreId = getCommitStoreId(props);
		state.commit = GithubCommitStore.getState(state.commitStoreId).commit;
	}

	state.repoStoreId = getRepoStoreId(props);
	state.repo = GithubRepoStore.getState(state.repoStoreId).repo;

	var jobOutputState;
	if (props.job) {
		state.jobOutputStoreId = getJobOutputStoreId(props);
		jobOutputState = JobOutputStore.getState(state.jobOutputStoreId);
		state.jobOutput = jobOutputState.output;
		state.jobError = jobOutputState.streamError;

		if (jobOutputState.open === false) {
			state.launching = false;
		} else {
			state.launching = true;
		}
	}

	state.launchComplete = props.appId && !state.launching && !state.jobError && !props.errorMsg;
	state.launchDisabled = props.launchDisabled || !!(!state.repo || !(state.commit || state.pull) || state.launching);

	if (props.errorMsg) {
		state.launchDisabled = false;
		state.launching = false;
	}

	return state;
}

Dashboard.Views.GithubDeploy = React.createClass({
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
						<Dashboard.Views.GithubCommit commit={commit} />
					) : (pull ? (
						<Dashboard.Views.GithubPull pull={pull} />
					) : null)}
				</header>

				{this.props.children}

				<label>
					<span className="name">Name</span>
					<input type="text" value={this.state.name} onChange={this.__handleNameChange} />
				</label>

				<label>
					<span className="name">Postgres</span>
					<input type="checkbox" checked={this.state.db} onChange={this.__handleDbChange} />
				</label>

				<Dashboard.Views.EditEnv env={this.state.env} onChange={this.__handleEnvChange} />

				{this.state.jobOutput ? (
					<Dashboard.Views.CommandOutput outputStreamData={this.state.jobOutput} />
				) : null}

				{this.props.errorMsg ? (
					<div className="alert-error">{this.props.errorMsg}</div>
				) : null}

				{this.state.jobError ? (
					<div className="alert-error">{this.state.jobError}</div>
				) : null}

				{this.state.launchComplete ? (
					<RouteLink className="launch-btn" path={this.props.getAppPath()}>Continue</RouteLink>
				) : (
					<button className="launch-btn" disabled={this.state.launchDisabled} onClick={this.__handleLaunchBtnClick}>{this.state.launching ? "Launching..." : "Launch app"}</button>
				)}
			</Modal>
		);
	},

	getInitialState: function () {
		return Marbles.Utils.extend(getState(this.props), {
			name: this.__formatName([this.props.ownerLogin, this.props.repoName, this.props.branchName].join("-")),
			db: false,
			env: {}
		});
	},

	componentDidMount: function () {
		GithubRepoStore.addChangeListener(this.state.repoStoreId, this.__handleStoreChange);
		if (this.state.commitStoreId) {
			GithubCommitStore.addChangeListener(this.state.commitStoreId, this.__handleStoreChange);
		}
		if (this.state.pullStoreId) {
			GithubPullStore.addChangeListener(this.state.pullStoreId, this.__handleStoreChange);
		}
		if (this.state.jobOutputStoreId) {
			JobOutputStore.addChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
		}
	},

	componentWillReceiveProps: function (props) {
		if (props.env) {
			this.setState({
				env: props.env
			});
		}

		var didChange = false;

		if (props.errorMsg) {
			didChange = true;
		}

		var prevRepoStoreId = this.state.repoStoreId;
		var nextRepoStoreId = getRepoStoreId(props);
		if ( !Marbles.Utils.assertEqual(prevRepoStoreId, nextRepoStoreId) ) {
			GithubRepoStore.addChangeListener(prevRepoStoreId, this.__handleStoreChange);
			GithubRepoStore.removeChangeListener(nextRepoStoreId, this.__handleStoreChange);
			didChange = true;
		}

		if (this.state.commitStoreId) {
			var prevCommitStoreId = this.state.commitStoreId;
			var nextCommitStoreId = getCommitStoreId(props);
			if ( !Marbles.Utils.assertEqual(prevCommitStoreId, nextCommitStoreId) ) {
				GithubCommitStore.removeChangeListener(prevCommitStoreId, this.__handleStoreChange);
				GithubCommitStore.addChangeListener(nextCommitStoreId, this.__handleStoreChange);
				didChange = true;
			}
		}

		if (this.state.pullStoreId) {
			var prevPullStoreId = this.state.pullStoreId;
			var nextPullStoreId = getPullStoreId(props);
			if ( !Marbles.Utils.assertEqual(prevPullStoreId, nextPullStoreId) ) {
				GithubPullStore.removeChangeListener(prevPullStoreId, this.__handleStoreChange);
				GithubPullStore.addChangeListener(nextPullStoreId, this.__handleStoreChange);
				didChange = true;
			}
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
		GithubRepoStore.removeChangeListener(this.state.repoStoreId, this.__handleStoreChange);
		if (this.state.commitStoreId) {
			GithubCommitStore.removeChangeListener(this.state.commitStoreId, this.__handleStoreChange);
		}
		if (this.state.pullStoreId) {
			GithubPullStore.removeChangeListener(this.state.pullStoreId, this.__handleStoreChange);
		}
		if (this.state.jobOutputStoreId) {
			JobOutputStore.removeChangeListener(this.state.jobOutputStoreId, this.__handleStoreChange);
		}
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props, this.state));
	},

	__handleNameChange: function (e) {
		var name = e.target.value;
		this.setState({
			name: this.__formatName(name)
		});
	},

	__handleDbChange: function (e) {
		var db = e.target.checked;
		this.setState({
			db: db
		});
	},

	__handleEnvChange: function (env) {
		this.setState({
			env: env
		});
	},

	__handleLaunchBtnClick: function (e) {
		e.preventDefault();
		var appData = Marbles.Utils.extend({
			name: this.state.name,
			dbRequested: this.state.db,
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
			GithubDeployActions.launchFromPull(this.state.repo, this.state.pull, appData);
		} else {
			GithubDeployActions.launchFromCommit(this.state.repo, this.props.branchName, this.state.commit, appData);
		}
	},

	__formatName: function (name) {
		name = name.replace(/[^-a-z\d]/gi, '').replace(/^[^a-z\d]/i, '');
		name = name.toLowerCase().substr(0, 30);
		return name;
	}
});

})();
