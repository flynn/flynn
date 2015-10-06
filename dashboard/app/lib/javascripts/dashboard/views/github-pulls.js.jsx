import { assertEqual } from 'marbles/utils';
import ScrollPagination from 'ScrollPagination';
import GithubPullsStore from '../stores/github-pulls';
import GithubPullsActions from '../actions/github-pulls';
import GithubPull from './github-pull';

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

var GithubPulls = React.createClass({
	displayName: "Views.GithubPulls",

	render: function () {
		var PullRequest = this.props.pullRequestComponent || GithubPull;
		var pullRequestProps = this.props.pullRequestProps || {};

		return (
			<section className="github-pulls">
				<ScrollPagination
					manager={this.props.scrollPaginationManager}
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
								manager={this.props.scrollPaginationManager}
								id={page.id}
								component='ul'>

								{page.pulls.map(function (pull) {
									return (
										<li key={pull.id}>
											<PullRequest
												pull={pull}
												pullStoreId={this.state.pullsStoreId}
												{...pullRequestProps} />
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

	getDefaultProps: function () {
		return {
			scrollPaginationManager: new ScrollPagination.Manager()
		};
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
		if ( !assertEqual(oldPullsStoreId, newPullsStoreId) ) {
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
	}
});

export default GithubPulls;
