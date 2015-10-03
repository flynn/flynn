import { assertEqual, extend } from 'marbles/utils';
import Modal from 'Modal';
import GithubRepoStore from '../stores/github-repo';
import GithubPullStore from '../stores/github-pull';
import AppSourceHistoryActions from '../actions/app-source-history';
import RouteLink from './route-link';
import GithubBranchSelector from './github-branch-selector';
import GithubCommitSelector from './github-commit-selector';
import GithubPull from './github-pull';
import GithubPulls from './github-pulls';

function getRepoStoreId (props) {
	var meta = props.release.meta;
	return {
		ownerLogin: meta.github_user,
		name: meta.github_repo
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
	return extend(state, GithubPullStore.getState(state.pullStoreId));
}

function getState (props) {
	var state = {
		repoStoreId: getRepoStoreId(props)
	};

	state.repo = GithubRepoStore.getState(state.repoStoreId).repo;

	return state;
}

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

					<GithubPull pull={pull} />

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
		var state = extend({}, pullState);

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
		if ( !assertEqual(prevPullStoreId, nextPullStoreId) ) {
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

var PullRequest = React.createClass({
	displayName: "Views.AppSourceHistory PullRequest",

	render: function () {
		var pull = this.props.pull;
		var base = pull.base;
		return (
			<GithubPull pull={pull}>
				<div className="merge-btn-container">
					<button className="merge-btn" onClick={this.props.merge.bind(null, pull)}>Merge into {base.ref}</button>
				</div>
			</GithubPull>
		);
	}
});

var AppSourceHistory = React.createClass({
	displayName: "Views.AppSourceHistory",

	render: function () {
		var getAppPath = this.props.getAppPath;

		var app = this.props.app;
		var meta = this.props.release.meta;
		var repo = this.state.repo;

		var ownerLogin = meta.github_user;
		var repoName = meta.github_repo;
		var selectedSha = this.props.selectedSha || meta.rev;
		var selectedBranchName = this.props.selectedBranchName || meta.branch;

		var deployBtnDisabled = true;
		if (selectedSha !== meta.rev) {
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
					<GithubBranchSelector
						ownerLogin={ownerLogin}
						repoName={repoName}
						selectedBranchName={selectedBranchName}
						defaultBranchName={repo ? repo.defaultBranch : null}
						deployedBranchName={meta.branch} />
				) : null}

				{selectedTab === "commits" ? (
					<GithubCommitSelector
						key={app.release}
						ownerLogin={ownerLogin}
						repoName={repoName}
						selectedBranchName={selectedBranchName}
						selectableCommits={true}
						selectedSha={selectedSha}
						deployedSha={meta.rev}
						deployedBranchName={meta.branch} />
				) : null}

				{selectedTab === "commits" ? (
					<div className="deploy-btn-container">
						<button className="btn-green" disabled={deployBtnDisabled} onClick={this.__handleDeployBtnClick}>Deploy</button>
					</div>
				) : null}

				{selectedTab === "pulls" ? (
					<GithubPulls
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
		if ( !assertEqual(oldRepoStoreId, newRepoStoreId) ) {
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
		var meta = this.props.release.meta;
		AppSourceHistoryActions.confirmDeployCommit(this.props.appId, meta.github_user, meta.github_repo, this.props.selectedBranchName || meta.branch, this.props.selectedSha);
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

export default AppSourceHistory;
