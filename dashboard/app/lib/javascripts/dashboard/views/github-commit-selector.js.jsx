import { assertEqual } from 'marbles/utils';
import ScrollPagination from 'ScrollPagination';
import GithubCommitsStore from '../stores/github-commits';
import GithubCommitsActions from '../actions/github-commits';
import GithubCommit from './github-commit';

function getCommitsStoreId (props) {
	var id = {
		ownerLogin: props.ownerLogin,
		repoName: props.repoName,
		branch: props.selectedBranchName
	};
	if (props.deployedSha && props.deployedBranchName === props.selectedBranchName) {
		id.refSha = props.deployedSha;
	}
	return id;
}

function getState (props, prevState) {
	prevState = prevState || {};
	var state = {
		commitsStoreId: getCommitsStoreId(props)
	};

	var commitsState = GithubCommitsStore.getState(state.commitsStoreId);
	state.commitsEmpty = commitsState.empty;
	state.commitsPages = commitsState.pages;
	state.commitsHasPrevPage = !!commitsState.prevPageParams;
	state.commitsHasNextPage = !!commitsState.nextPageParams;

	var hasDeployedCommit = false;
	for (var i = 0, ref = state.commitsPages, len = ref.length; i < len; i++) {
		if (ref[i].hasRefSha) {
			hasDeployedCommit = true;
			break;
		}
	}
	state.hasDeployedCommit = hasDeployedCommit || prevState.hasDeployedCommit;
	state.shouldScrollToDeployedCommit = state.hasDeployedCommit && !prevState.hasDeployedCommit;

	return state;
}

var GithubCommitSelector = React.createClass({
	displayName: "Views.GithubCommitSelector",

	render: function () {
		var Commit = this.props.commitComponent || GithubCommit;

		var deployedSha = this.props.deployedSha;
		var selectedSha = this.props.selectedSha;
		var selectableCommits = !!this.props.selectableCommits;

		return (
			<section className="github-commits">
				<ScrollPagination
					manager={this.props.scrollPaginationManager}
					hasPrevPage={this.state.commitsHasPrevPage}
					hasNextPage={this.state.commitsHasNextPage}
					unloadPage={GithubCommitsActions.unloadPageId.bind(null, this.state.commitsStoreId)}
					loadPrevPage={GithubCommitsActions.fetchPrevPage.bind(null, this.state.commitsStoreId)}
					loadNextPage={GithubCommitsActions.fetchNextPage.bind(null, this.state.commitsStoreId)}>

					{this.state.commitsEmpty ? (
						<p className="placeholder">There are no commits</p>
					) : null}

					{this.state.commitsPages.map(function (page) {
						return (
							<ScrollPagination.Page
								key={page.id}
								manager={this.props.scrollPaginationManager}
								id={page.id}
								component='ul'>

								{page.commits.map(function (commit) {
									return (
										<li key={commit.sha} className={commit.sha === deployedSha ? "deployed" : ""}>
											<Commit
												ref={commit.sha}
												commit={commit}
												selectable={selectableCommits}
												selected={commit.sha === selectedSha}
												commitsStoreId={this.state.commitsStoreId}
												onSelect={this.__handleCommitSelected} />
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
		GithubCommitsStore.addChangeListener(this.state.commitsStoreId, this.__handleStoreChange);
	},

	componentDidUpdate: function () {
		if (this.state.shouldScrollToDeployedCommit) {
			var component = this.refs[this.props.deployedSha];
			if (component && component.isMounted()) {
				component.scrollIntoView();
			}
			this.setState({ shouldScrollToDeployedCommit: false });
		}
	},

	componentWillReceiveProps: function (props) {
		var oldCommitsStoreId = this.state.commitsStoreId;
		var newCommitsStoreId = getCommitsStoreId(props);
		if ( !assertEqual(oldCommitsStoreId, newCommitsStoreId) ) {
			GithubCommitsStore.removeChangeListener(oldCommitsStoreId, this.__handleStoreChange);
			GithubCommitsStore.addChangeListener(newCommitsStoreId, this.__handleStoreChange);
			this.__handleStoreChange(props);
		}
	},

	componentWillUnmount: function () {
		GithubCommitsStore.removeChangeListener(this.state.commitsStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props, this.state));
	},

	__handleCommitSelected: function (commit) {
		GithubCommitsActions.commitSelected(this.state.commitsStoreId, commit.sha);
	}
});

export default GithubCommitSelector;
