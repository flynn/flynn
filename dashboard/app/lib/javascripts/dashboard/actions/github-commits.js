import Dispatcher from '../dispatcher';

var GithubCommits = {
	unloadPageId: function (storeId, pageId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_COMMITS:UNLAOD_PAGE_ID",
			storeId: storeId,
			pageId: pageId
		});
	},

	fetchPrevPage: function (storeId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_COMMITS:FETCH_PREV_PAGE",
			storeId: storeId
		});
	},

	fetchNextPage: function (storeId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_COMMITS:FETCH_NEXT_PAGE",
			storeId: storeId
		});
	},

	launchCommit: function (storeId, sha) {
		Dispatcher.handleViewAction({
			name: "GITHUB_COMMITS:LAUNCH_COMMIT",
			storeId: storeId,
			sha: sha
		});
	},

	commitSelected: function (storeId, sha) {
		Dispatcher.handleViewAction({
			name: "GITHUB_COMMITS:COMMIT_SELECTED",
			storeId: storeId,
			sha: sha
		});
	}
};

export default GithubCommits;
