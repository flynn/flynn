import Dispatcher from '../dispatcher';

var GithubRepos = {
	unloadPageId: function (storeId, pageId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_REPOS:UNLAOD_PAGE_ID",
			storeId: storeId,
			pageId: pageId
		});
	},

	fetchPrevPage: function (storeId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_REPOS:FETCH_PREV_PAGE",
			storeId: storeId
		});
	},

	fetchNextPage: function (storeId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_REPOS:FETCH_NEXT_PAGE",
			storeId: storeId
		});
	}
};

export default GithubRepos;
