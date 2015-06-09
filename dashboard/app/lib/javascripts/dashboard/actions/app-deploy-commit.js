import Dispatcher from '../dispatcher';

var AppDeployCommit = {
	deployCommit: function (appId, ownerLogin, repoName, branchName, sha) {
		Dispatcher.handleViewAction({
			name: "APP_DEPLOY_COMMIT:DEPLOY_COMMIT",
			storeId: {
				appId: appId
			},
			ownerLogin: ownerLogin,
			repoName: repoName,
			branchName: branchName,
			sha: sha
		});
	}
};

export default AppDeployCommit;
