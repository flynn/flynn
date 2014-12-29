//= require ../stores/github-repos
//= require ../stores/github-repo
//= require ../stores/github-pull
//= require ../actions/app-source-history
//= require ./github-branch-selector
//= require ./github-commit-selector
//= require ./github-pulls
//= require ./route-link
//= require Modal

(function () {

"use strict";

var GithubRepoStore = Dashboard.Stores.GithubRepo;
var GithubPullStore = Dashboard.Stores.GithubPull;

var AppSourceHistoryActions = Dashboard.Actions.AppSourceHistory;

var RouteLink = Dashboard.Views.RouteLink;
var Modal = window.Modal;

function getRepoStoreId (props) {
	var meta = props.app.meta;
	return {
		ownerLogin: meta.user_login,
		name: meta.repo_name
	};
}

function getPullStoreId (pull) {
	var base = pull.base;
	return {
		ownerLogin: base.ownerLogin,
		repoName: base.name,
		number: pull.number
	};
}

function getPullState (pull) {
	var state = {
		pullStoreId: getPullStoreId(pull)
	};
	return Marbles.Utils.extend(state, GithubPullStore.getState(state.pullStoreId));
}

function getState (props) {
	var state = {
		repoStoreId: getRepoStoreId(props)
	};

	state.repo = GithubRepoStore.getState(state.repoStoreId).repo;

	return state;
}

Dashboard.Views.AppSourceHistory = React.createClass({
	displayName: "Views.AppSourceHistory",

	render: function () {
		var getAppPath = this.props.getAppPath;

		var app = this.props.app;
		var meta = app.meta;
		var repo = this.state.repo;

		var ownerLogin = meta.user_login;
		var repoName = meta.repo_name;
		var selectedSha = this.props.selectedSha || meta.sha;
		var selectedBranchName = this.props.selectedBranchName || meta.ref;

		var deployBtnDisabled = true;
		if (selectedSha !== meta.sha) {
			deployBtnDisabled = false;
		}

		var selectedTab = this.props.selectedTab || "commits";

		return (
			<div className="app-source-history">
				<header>
					<h2>Source history</h2>

					<nav>
						<ul className="h-nav">
							<li className={selectedTab === "commits" ? "selected" : null}>
								<RouteLink path={getAppPath("", { shtab: null })}>
									Commits
								</RouteLink>
							</li>

							<li className={selectedTab === "pulls" ? "selected" : null}>
								<RouteLink path={getAppPath("", { shtab: "pulls" })}>
									Pull requests
								</RouteLink>
							</li>
						</ul>
					</nav>
				</header>

				{selectedTab === "commits" ? (
					<Dashboard.Views.GithubBranchSelector
						ownerLogin={ownerLogin}
						repoName={repoName}
						selectedBranchName={selectedBranchName}
						defaultBranchName={repo ? repo.defaultBranch : null}
						deployedBranchName={meta.ref} />
				) : null}

				{selectedTab === "commits" ? (
					<Dashboard.Views.GithubCommitSelector
						ownerLogin={ownerLogin}
						repoName={repoName}
						selectedBranchName={selectedBranchName}
						selectableCommits={true}
						selectedSha={selectedSha}
						deployedSha={meta.sha}
						deployedBranchName={meta.ref} />
				) : null}

				{selectedTab === "commits" ? (
					<div className="deploy-btn-container">
						<button className="btn-green" disabled={deployBtnDisabled} onClick={this.__handleDeployBtnClick}>Deploy</button>
					</div>
				) : null}

				{selectedTab === "pulls" ? (
					<Dashboard.Views.GithubPulls
						ownerLogin={ownerLogin}
						repoName={repoName}
						pullRequestComponent={PullRequest}
						pullRequestProps={{merge: this.__handleMergeBtnClick}} />
				) : null}

				{this.state.confirmMergePull ? (
					<ConfirmMergePull
						pull={this.state.confirmMergePull}
						onHide={this.__handleMergeConfirmDismiss} />
				) : null}
			</div>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		GithubRepoStore.addChangeListener(this.state.repoStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var oldRepoStoreId = this.state.repoStoreId;
		var newRepoStoreId = getRepoStoreId(props);
		if ( !Marbles.Utils.assertEqual(oldRepoStoreId, newRepoStoreId) ) {
			GithubRepoStore.removeChangeListener(oldRepoStoreId, this.__handleStoreChange);
			GithubRepoStore.addChangeListener(newRepoStoreId, this.__handleStoreChange);
			this.__handleStoreChange(props);
		}
	},

	componentWillUnmount: function () {
		GithubRepoStore.removeChangeListener(this.state.repoStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props));
	},

	__handleDeployBtnClick: function (e) {
		e.preventDefault();
		if ( !this.props.selectedSha ) {
			return;
		}
		var app = this.props.app;
		var meta = app.meta;
		AppSourceHistoryActions.confirmDeployCommit(this.props.appId, meta.user_login, meta.repo_name, this.props.selectedBranchName || meta.ref, this.props.selectedSha);
	},

	__handleMergeBtnClick: function (pull) {
		this.setState({
			confirmMergePull: pull
		});
	},

	__handleMergeConfirmDismiss: function () {
		this.setState({
			confirmMergePull: null
		});
	}
});

var PullRequest = React.createClass({
	displayName: "Views.AppSourceHistory PullRequest",

	render: function () {
		var pull = this.props.pull;
		var base = pull.base;
		return (
			<Dashboard.Views.GithubPull pull={pull}>
				<div className="merge-btn-container">
					<button className="merge-btn" onClick={this.props.merge.bind(null, pull)}>Merge into {base.ref}</button>
				</div>
			</Dashboard.Views.GithubPull>
		);
	}
});

var ConfirmMergePull = React.createClass({
	displayName: "Views.AppSourceHistory ConfirmMergePull",

	render: function () {
		var pull = this.props.pull;
		var base = pull.base;

		var mergeJob = this.state.mergeJob;

		return (
			<Modal visible={ !mergeJob || mergeJob.status !== "success" } onShow={function(){}} onHide={this.props.onHide}>
				<section>
					<header>
						<h1>Merge into {base.ref}?</h1>
					</header>

					<Dashboard.Views.GithubPull pull={pull} />

					{mergeJob && mergeJob.errorMsg ? (
						<div className="alert-error">{mergeJob.errorMsg}</div>
					) : null}

					<button
						className="merge-confirm-btn"
						onClick={this.__handleMergeConfirmBtnClick}
						disabled={this.state.mergeBtnDisabled}>
						{mergeJob && mergeJob.status === "pending" ? (
							"Merging into "+ base.fullName +":"+ base.ref
						) : (
							"Merge into "+ base.fullName +":"+ base.ref
						)}
					</button>
				</section>
			</Modal>
		);
	},

	getState: function (props) {
		var pullState = getPullState(props.pull);
		var state = Marbles.Utils.extend({}, pullState);

		var mergeJob = state.mergeJob;
		state.mergeBtnDisabled = false;
		if (mergeJob && (mergeJob.status === "pending" || mergeJob.status === "success")) {
			state.mergeBtnDisabled = true;
		}

		return state;
	},

	getInitialState: function () {
		return this.getState(this.props);
	},

	componentDidMount: function () {
		GithubPullStore.addChangeListener(this.state.pullStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevPullStoreId = this.state.pullStoreId;
		var nextPullStoreId = getPullStoreId(nextProps.pull);
		if ( !Marbles.Utils.assertEqual(prevPullStoreId, nextPullStoreId) ) {
			GithubPullStore.removeChangeListener(prevPullStoreId, this.__handleStoreChange);
			GithubPullStore.addChangeListener(nextPullStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		GithubPullStore.removeChangeListener(this.state.pullStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(this.getState(props || this.props));
	},

	__handleMergeConfirmBtnClick: function (e) {
		e.preventDefault();
		AppSourceHistoryActions.mergePullRequest(this.props.pull);
	}
});

})();
