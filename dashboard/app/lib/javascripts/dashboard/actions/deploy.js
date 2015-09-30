import { extend } from 'marbles/utils';
import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';

var createTaffyJob = function (taffyReleaseId, appID, appName, meta) {
	var client = Config.client;

	var cloneURL = meta.clone_url;
	var branch = meta.branch;
	var rev = meta.rev;
	return client.createTaffyJob({
		release: taffyReleaseId,
		release_env: true,
		cmd: [appName, cloneURL, branch, rev],
		meta: extend({}, meta, {
			app: appID
		})
	});
};

var deployFromGithub = function (meta, appData) {
	var client = Config.client;

	var appId, appName;
	var databaseEnv = {};

	function createDatabase () {
		return client.createAppDatabase({ apps: [appId] }).then(function (args) {
			var res = args[0];
			databaseEnv = res.env;
			return createRelease();
		});
	}

	function createRelease () {
		return client.createRelease({
			env: extend({}, appData.env, databaseEnv),
			meta: meta
		}).then(function (args) {
			var res = args[0];
			return createAppRelease(res.id);
		});
	}

	function createAppRelease (releaseId) {
		return client.createAppRelease(appId, {
			id: releaseId
		}).then(function () {
			return getTaffyRelease();
		});
	}

	function getTaffyRelease () {
		return client.getTaffyRelease().then(function (args) {
			var res = args[0];
			return createTaffyJob(res.id, appId, appName, meta);
		});
	}

	function findOrCreateApp() {
		if (appData.hasOwnProperty('id')) {
			return client.getAppRelease(appData.id).then(function (args) {
				appData.env = args[0].env;
				return client.getApp(appData.id);
			});
		} else {
			return client.createApp({
				name: appData.name
			});
		}
	}

	return findOrCreateApp().then(function (args) {
		var res = args[0];
		appId = res.id;
		appName = res.name;
		if (appData.dbRequested) {
			return createDatabase();
		} else {
			return createRelease();
		}
	});
};

var deployFromGithubCommit = function (repo, branchName, sha, appData) {
	var meta = {
		github: 'true',
		github_user: repo.ownerLogin,
		github_repo: repo.name,
		branch: branchName,
		rev: sha,
		clone_url: repo.cloneURL
	};
	return deployFromGithub(meta, appData);
};

var deployFromGithubPull = function (repo, pull, appData) {
	var meta = {
		github: 'true',
		github_user: repo.ownerLogin,
		github_repo: repo.name,
		branch: pull.head.ref,
		rev: pull.head.sha,
		clone_url: repo.cloneURL,
		pull_number: String(pull.number),
		pull_user: pull.user.login,
		base_user: pull.base.ownerLogin,
		base_repo: pull.base.name,
		base_branch: pull.base.ref,
		base_rev: pull.base.sha
	};
	return deployFromGithub(meta, appData);
};

Dispatcher.register(function (event) {
	switch (event.name) {
		case 'DEPLOY_APP':
			switch (event.source) {
				case 'GH_COMMIT':
					deployFromGithubCommit(event.repo, event.branchName, event.commit.sha, event.appData);
				break;
				case 'GH_PULL':
					deployFromGithubPull(event.repo, event.pull, event.appData);
				break;
				default:
					throw new Error('Unknown source for DEPLOY_APP action: '+ JSON.stringify(event.source));
			}
		break;

		case 'APP_DEPLOY_COMMIT':
			deployFromGithub({
				github: 'true',
				github_user: event.ownerLogin,
				github_repo: event.repoName,
				branch: event.branchName,
				rev: event.sha,
				clone_url: event.repo.cloneURL
			}, {
				id: event.appID
			});
		break;

		case 'APP_DEPLOY_RELEASE':
			Config.client.deployAppRelease(event.appID, event.releaseID);
		break;
	}
});
