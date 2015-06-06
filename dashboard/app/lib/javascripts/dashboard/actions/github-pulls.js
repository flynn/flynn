import Dispatcher from '../dispatcher';

var GithubPulls = {
	unloadPageId: function (storeId, pageId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_PULLS:UNLAOD_PAGE_ID",
			storeId: storeId,
			pageId: pageId
		});
	},

	fetchPrevPage: function (storeId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_PULLS:FETCH_PREV_PAGE",
			storeId: storeId
		});
	},

	fetchNextPage: function (storeId) {
		Dispatcher.handleViewAction({
			name: "GITHUB_PULLS:FETCH_NEXT_PAGE",
			storeId: storeId
		});
	},

	launchPull: function (storeId, pull) {
		Dispatcher.handleViewAction({
			name: "GITHUB_PULLS:LAUNCH_PULL",
			storeId: storeId,
			pull: pull
		});
	}
};

export default GithubPulls;
