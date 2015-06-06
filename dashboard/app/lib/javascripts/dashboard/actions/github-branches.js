import Dispatcher from '../dispatcher';

var GithubBranches = {
	branchSelected: function (storeId, branchName) {
		Dispatcher.handleViewAction({
			name: "GITHUB_BRANCH_SELECTOR:BRANCH_SELECTED",
			storeId: storeId,
			branchName: branchName
		});
	}
};

export default GithubBranches;
