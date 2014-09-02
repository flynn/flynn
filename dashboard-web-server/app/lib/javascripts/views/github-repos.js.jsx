/** @jsx React.DOM */
//= require ../stores/github-user
//= require ../stores/github-repos
//= require ../actions/github-repos
//= require ./helpers/getPath
//= require ./route-link
//= require ScrollPagination

(function () {

"use strict";

var ScrollPagination = window.ScrollPagination;

var GithubUserStore = FlynnDashboard.Stores.GithubUser;
var GithubReposStore = FlynnDashboard.Stores.GithubRepos;

var GithubReposActions = FlynnDashboard.Actions.GithubRepos;

var userStoreId = "default";

var getPath = FlynnDashboard.Views.Helpers.getPath;

function getRepoStoreId(props) {
	return {
		org: props.selectedSource,
		type: props.selectedType
	};
}

function getState(props) {
	var state = {};

	state.reposStoreId = getRepoStoreId(props);

	var reposState = GithubReposStore.getState(state.reposStoreId);
	state.reposPages = reposState.pages;
	state.reposHasPrevPage = !!reposState.prevPageParams;
	state.reposHasNextPage = !!reposState.nextPageParams;

	return state;
}

function getTypesState() {
	var state = {};

	state.user = GithubUserStore.getState(userStoreId).user;

	return state;
}

FlynnDashboard.Views.GithubRepos = React.createClass({
	displayName: "Views.GithubRepos",

	render: function () {
		var handlePageEvent = this.__handlePageEvent;

		return (
			<div>
				<Types selectedType={this.props.selectedType} selectedSource={this.props.selectedSource} />

				<ScrollPagination
					ref="scrollPagination"
					hasPrevPage={this.state.reposHasPrevPage}
					hasNextPage={this.state.reposHasNextPage}
					unloadPage={GithubReposActions.unloadPageId.bind(null, this.state.reposStoreId)}
					loadPrevPage={GithubReposActions.fetchPrevPage.bind(null, this.state.reposStoreId)}
					loadNextPage={GithubReposActions.fetchNextPage.bind(null, this.state.reposStoreId)}>

					{this.state.reposPages.map(function (page) {
						return (
							<ScrollPagination.Page
								key={page.id}
								id={page.id}
								className="github-repos"
								onPageEvent={handlePageEvent}
								component={React.DOM.ul}>

								{page.repos.map(function (repo) {
									return (
										<li key={repo.id}>
											<FlynnDashboard.Views.RouteLink path={getPath([{ repo: repo.name, owner: repo.ownerLogin, branch: repo.defaultBranch }])}>
												<h2>
													{repo.name} <small>{repo.language}</small>
												</h2>
												<p>{repo.description}</p>
											</FlynnDashboard.Views.RouteLink>
										</li>
									);
								}, this)}
							</ScrollPagination.Page>
						);
					}, this)}
				</ScrollPagination>
			</div>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		GithubReposStore.addChangeListener(this.state.reposStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var oldRepoStoreId = this.state.reposStoreId;
		var newRepoStoreId = getRepoStoreId(props);
		if ( !Marbles.Utils.assertEqual(oldRepoStoreId, newRepoStoreId) ) {
			GithubReposStore.removeChangeListener(oldRepoStoreId, this.__handleStoreChange);
			GithubReposStore.addChangeListener(newRepoStoreId, this.__handleStoreChange);
		}
		this.setState(getState(props));
	},

	componentWillUnmount: function () {
		GithubReposStore.removeChangeListener(this.state.reposStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(getState(this.props));
	},

	__handlePageEvent: function (pageId, event) {
		this.refs.scrollPagination.handlePageEvent(pageId, event);
	}
});

var Types = React.createClass({
	displayName: "Views.GithubRepos - Types",

	render: function () {
		var user = this.state.user;
		return (
			<section className="github-repo-types">
				<ul>
					<li className={this.props.selectedType === null ? "selected" : null}>
						<FlynnDashboard.Views.RouteLink path={getPath([{ type: null }])}>
							{this.props.selectedSource || (user ? user.login : "")}
						</FlynnDashboard.Views.RouteLink>
					</li>

					{this.props.selectedSource ? null : (
						<li className={this.props.selectedType === "star" ? "selected" : null}>
							<FlynnDashboard.Views.RouteLink path={getPath([{ type: "star" }])}>
								starred
							</FlynnDashboard.Views.RouteLink>
						</li>
					)}

					<li className={this.props.selectedType === "fork" ? "selected" : null}>
						<FlynnDashboard.Views.RouteLink path={getPath([{ type: "fork" }])}>
							forked
						</FlynnDashboard.Views.RouteLink>
					</li>
				</ul>
			</section>
		);
	},

	getInitialState: function () {
		return getTypesState(this.props);
	},

	componentDidMount: function () {
		GithubUserStore.addChangeListener(userStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		this.setState(getTypesState(props));
	},

	componentWillUnmount: function () {
		GithubUserStore.removeChangeListener(userStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(getTypesState(this.props));
	}
});

})();
