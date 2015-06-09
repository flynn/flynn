import { assertEqual } from 'marbles/utils';
import ScrollPagination from 'ScrollPagination';
import GithubUserStore from '../stores/github-user';
import GithubReposStore from '../stores/github-repos';
import GithubReposActions from '../actions/github-repos';
import getPath from './helpers/getPath';
import RouteLink from './route-link';

var userStoreId = "default";

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

var Types = React.createClass({
	displayName: "Views.GithubRepos - Types",

	render: function () {
		var user = this.state.user;
		return (
			<section className="github-repo-types">
				<ul>
					<li className={this.props.selectedType === null ? "selected" : null}>
						<RouteLink path={getPath([{ type: null }])}>
							{this.props.selectedSource || (user ? user.login : "")}
						</RouteLink>
					</li>

					{this.props.selectedSource ? null : (
						<li className={this.props.selectedType === "star" ? "selected" : null}>
							<RouteLink path={getPath([{ type: "star" }])}>
								starred
							</RouteLink>
						</li>
					)}

					<li className={this.props.selectedType === "fork" ? "selected" : null}>
						<RouteLink path={getPath([{ type: "fork" }])}>
							forked
						</RouteLink>
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

var GithubRepos = React.createClass({
	displayName: "Views.GithubRepos",

	render: function () {
		return (
			<div>
				<Types selectedType={this.props.selectedType} selectedSource={this.props.selectedSource} />

				<ScrollPagination
					key={this.state.reposStoreId}
					manager={this.props.scrollPaginationManager}
					hasPrevPage={this.state.reposHasPrevPage}
					hasNextPage={this.state.reposHasNextPage}
					unloadPage={GithubReposActions.unloadPageId.bind(null, this.state.reposStoreId)}
					loadPrevPage={GithubReposActions.fetchPrevPage.bind(null, this.state.reposStoreId)}
					loadNextPage={GithubReposActions.fetchNextPage.bind(null, this.state.reposStoreId)}>

					{this.state.reposPages.map(function (page) {
						return (
							<ScrollPagination.Page
								key={page.id}
								manager={this.props.scrollPaginationManager}
								id={page.id}
								className="github-repos"
								component='ul'>

								{page.repos.map(function (repo) {
									return (
										<li key={repo.id}>
											<RouteLink path={getPath([{ repo: repo.name, owner: repo.ownerLogin, branch: repo.defaultBranch }])}>
												<h2>
													{repo.name} <small>{repo.language}</small>
												</h2>
												<p>{repo.description}</p>
											</RouteLink>
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

	getDefaultProps: function () {
		return {
			scrollPaginationManager: new ScrollPagination.Manager()
		};
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
		if ( !assertEqual(oldRepoStoreId, newRepoStoreId) ) {
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

export default GithubRepos;
