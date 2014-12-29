//= require ../stores/github-pulls
//= require ../actions/github-pulls
//= require ./github-pull
//= require ScrollPagination

(function () {

"use strict";

var GithubPullsStore = Dashboard.Stores.GithubPulls;
var GithubPullsActions = Dashboard.Actions.GithubPulls;

var ScrollPagination = window.ScrollPagination;

function getPullsStoreId (props) {
	return {
		ownerLogin: props.ownerLogin,
		repoName: props.repoName
	};
}

function getState (props) {
	var state = {
		pullsStoreId: getPullsStoreId(props)
	};

	var pullsState = GithubPullsStore.getState(state.pullsStoreId);
	state.pullsEmpty = pullsState.empty;
	state.pullsPages = pullsState.pages;
	state.pullsHasPrevPage = !!pullsState.prevPageParams;
	state.pullsHasNextPage = !!pullsState.nextPageParams;

	return state;
}

Dashboard.Views.GithubPulls = React.createClass({
	displayName: "Views.GithubPulls",

	render: function () {
		var PullRequest = this.props.pullRequestComponent || Dashboard.Views.GithubPull;
		var pullRequestProps = this.props.pullRequestProps || {};
		var handlePageEvent = this.__handlePageEvent;

		return (
			<section className="github-pulls">
				<ScrollPagination
					ref="scrollPagination"
					hasPrevPage={this.state.pullsHasPrevPage}
					hasNextPage={this.state.pullsHasNextPage}
					unloadPage={GithubPullsActions.unloadPageId.bind(null, this.state.pullsStoreId)}
					loadPrevPage={GithubPullsActions.fetchPrevPage.bind(null, this.state.pullsStoreId)}
					loadNextPage={GithubPullsActions.fetchNextPage.bind(null, this.state.pullsStoreId)}>

					{this.state.pullsEmpty ? (
						<p className="placeholder">There are no open pull requests</p>
					) : null}

					{this.state.pullsPages.map(function (page) {
						return (
							<ScrollPagination.Page
								key={page.id}
								id={page.id}
								onPageEvent={handlePageEvent}
								component='ul'>

								{page.pulls.map(function (pull) {
									return (
										<li key={pull.id}>
											{PullRequest(Marbles.Utils.extend({
												pull: pull,
												pullStoreId: this.state.pullsStoreId
											}, pullRequestProps))}
										</li>
									);
								}, this)}
							</ScrollPagination.Page>
						);
					}, this)}
				</ScrollPagination>
			</section>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		GithubPullsStore.addChangeListener(this.state.pullsStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var oldPullsStoreId = this.state.pullsStoreId;
		var newPullsStoreId = getPullsStoreId(props);
		if ( !Marbles.Utils.assertEqual(oldPullsStoreId, newPullsStoreId) ) {
			GithubPullsStore.removeChangeListener(oldPullsStoreId, this.__handleStoreChange);
			GithubPullsStore.addChangeListener(newPullsStoreId, this.__handleStoreChange);
			this.__handleStoreChange(props);
		}
	},

	componentWillUnmount: function () {
		GithubPullsStore.removeChangeListener(this.state.pullsStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props));
	},

	__handlePageEvent: function (pageId, event) {
		this.refs.scrollPagination.handlePageEvent(pageId, event);
	}
});

})();
