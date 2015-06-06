import Dispatcher from '../dispatcher';

var GithubDeploy = {
	launchFromCommit: function (repo, branchName, commit, appData) {
		Dispatcher.handleViewAction({
			name: "GITHUB_DEPLOY:LAUNCH_FROM_COMMIT",
			repo: repo,
			branchName: branchName,
			commit: commit,
			appData: appData
		});
	},

	launchFromPull: function (repo, pull, appData) {
		Dispatcher.handleViewAction({
			name: "GITHUB_DEPLOY:LAUNCH_FROM_PULL",
			repo: repo,
			pull: pull,
			appData: appData
		});
	}
};

export default GithubDeploy;
