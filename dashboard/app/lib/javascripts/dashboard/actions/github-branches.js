//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = Dashboard.Dispatcher;

Dashboard.Actions.GithubBranches = {
	branchSelected: function (storeId, branchName) {
		Dispatcher.handleViewAction({
			name: "GITHUB_BRANCH_SELECTOR:BRANCH_SELECTED",
			storeId: storeId,
			branchName: branchName
		});
	}
};

})();
