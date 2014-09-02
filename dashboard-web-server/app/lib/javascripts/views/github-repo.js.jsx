/** @jsx React.DOM */
//= require ../stores/github-repo
//= require ../actions/github-commits
//= require ../actions/github-pulls
//= require ./github-pulls
//= require ./github-branch-selector
//= require ./github-commit-selector
//= require ./github-commit
//= require ./github-pull
//= require ./helpers/getPath
//= require ./route-link

(function () {

"use strict";

var GithubRepoStore = FlynnDashboard.Stores.GithubRepo;

var GithubCommitsActions = FlynnDashboard.Actions.GithubCommits;
var GithubPullsActions = FlynnDashboard.Actions.GithubPulls;

var getPath = FlynnDashboard.Views.Helpers.getPath;
var RouteLink = FlynnDashboard.Views.RouteLink;

function getRepoStoreId (props) {
	return {
		ownerLogin: props.ownerLogin,
		name: props.name
	};
}

function getState (props) {
	var state = {
		repoStoreId: getRepoStoreId(props)
	};

	state.repo = GithubRepoStore.getState(state.repoStoreId).repo;

	return state;
}

FlynnDashboard.Views.GithubRepo = React.createClass({
	displayName: "Views.GithubRepo",

	render: function () {
		var selectedPanel = this.props.selectedPanel;
		if ( !selectedPanel ) {
			selectedPanel = "commits";
		}

		var selectedBranchName = this.props.selectedBranchName;
		var repo = this.state.repo;
		if ( !selectedBranchName && repo ) {
			selectedBranchName = repo.defaultBranch;
		}

		return (
			<section className="github-repo">
				<header>
					<h1>{this.props.ownerLogin +"/"+ this.props.name}</h1>

					<ul className="h-nav">
						<li className={selectedPanel === "commits" ? "selected" : null}>
							<RouteLink path={getPath([{ repo_panel: "commits" }])}>
								Commits
							</RouteLink>
						</li>
						<li className={selectedPanel === "pulls" ? "selected" : null}>
							<RouteLink path={getPath([{ repo_panel: "pulls" }])}>
								Pull Requests
							</RouteLink>
						</li>
					</ul>
				</header>

				{selectedPanel === "commits" ? (
					<div>
						<FlynnDashboard.Views.GithubBranchSelector
							ownerLogin={this.props.ownerLogin}
							repoName={this.props.name}
							selectedBranchName={selectedBranchName}
							defaultBranchName={repo ? repo.defaultBranch : null}/>

						{repo ? (
							<FlynnDashboard.Views.GithubCommitSelector
								ownerLogin={this.props.ownerLogin}
								repoName={this.props.name}
								selectedBranchName={selectedBranchName}
								selectedSha={this.props.selectedSha}
								commitComponent={Commit} />
						) : null}
					</div>
				) : null}

				{selectedPanel === "pulls" ? (
					<FlynnDashboard.Views.GithubPulls
						ownerLogin={this.props.ownerLogin}
						repoName={this.props.name}
						pullRequestComponent={PullRequest}/>
				) : null}
			</section>
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
});

var Commit = React.createClass({
	displayName: "Views.GithubRepo Commit",

	render: function () {
		return (
			<FlynnDashboard.Views.GithubCommit commit={this.props.commit}>
				<div className="launch-btn-container">
					<button className="launch-btn" onClick={this.__handleLaunchBtnClick}>Launch</button>
				</div>
			</FlynnDashboard.Views.GithubCommit>
		);
	},

	__handleLaunchBtnClick: function (e) {
		e.preventDefault();
		GithubCommitsActions.launchCommit(this.props.commitsStoreId, this.props.commit.sha);
	}
});

var PullRequest = React.createClass({
	displayName: "Views.GithubRepo PullRequest",

	render: function () {
		return (
			<FlynnDashboard.Views.GithubPull pull={this.props.pull}>
				<div className="launch-btn-container">
					<button className="launch-btn" onClick={this.__handleLaunchBtnClick}>Launch</button>
				</div>
			</FlynnDashboard.Views.GithubPull>
		);
	},

	__handleLaunchBtnClick: function (e) {
		e.preventDefault();
		GithubPullsActions.launchPull(this.props.pullsStoreId, this.props.pull);
	}
});

})();
