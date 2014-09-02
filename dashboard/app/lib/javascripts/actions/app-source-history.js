//= require ../dispatcher

(function () {

"use strict";

var Dispatcher = FlynnDashboard.Dispatcher;

FlynnDashboard.Actions.AppSourceHistory = {
	confirmDeployCommit: function (appId, ownerLogin, repoName, branchName, sha) {
		Dispatcher.handleViewAction({
			name: "APP_SOURCE_HISTORY:CONFIRM_DEPLOY_COMMIT",
			storeId: {
				appId: appId
			},
			ownerLogin: ownerLogin,
			repoName: repoName,
			branchName: branchName,
			sha: sha
		});
	},

	mergePullRequest: function (pull) {
		var base = pull.base;
		Dispatcher.handleViewAction({
			name: "APP_SOURCE_HISTORY:MERGE_PULL_REQUEST",
			storeId: {
				ownerLogin: base.ownerLogin,
				repoName: base.name,
				number: pull.number
			}
		});
	}
};

})();
