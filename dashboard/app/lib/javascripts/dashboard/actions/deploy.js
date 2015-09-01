import { extend } from 'marbles/utils';
import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';

var createTaffyJob = function (taffyReleaseId, appID, appName, meta) {
	var client = Config.client;

	var cloneURL = meta.clone_url;
	var ref = meta.ref;
	var sha = meta.sha;
	return client.createTaffyJob({
		release: taffyReleaseId,
		release_env: true,
		cmd: [appName, cloneURL, ref, sha],
		meta: extend({}, meta, {
			app: appID,
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
			env: extend({}, appData.env, databaseEnv)
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

	function updateOrCreateApp() {
		if (appData.hasOwnProperty('id')) {
			return client.getApp(appData.id).then(function (args) {
				var res = args[0];
				meta = extend({}, res.meta, meta);
				return client.updateApp(res.id, {
					meta: meta
				});
			});
		} else {
			return client.createApp({
				name: appData.name,
				meta: meta
			});
		}
	}

	return updateOrCreateApp().then(function (args) {
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
		type: 'github',
		user_login: repo.ownerLogin,
		repo_name: repo.name,
		ref: branchName,
		sha: sha,
		clone_url: repo.cloneURL
	};
	return deployFromGithub(meta, appData);
};

var deployFromGithubPull = function (repo, pull, appData) {
	var meta = {
		type: 'github',
		user_login: repo.ownerLogin,
		repo_name: repo.name,
		ref: pull.head.ref,
		sha: pull.head.sha,
		clone_url: repo.cloneURL,
		pull_number: String(pull.number),
		pull_user_login: pull.user.login,
		base_user_login: pull.base.ownerLogin,
		base_repo_name: pull.base.name,
		base_ref: pull.base.ref,
		base_sha: pull.base.sha
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
				type: 'github',
				user_login: event.ownerLogin,
				repo_name: event.repoName,
				ref: event.branchName,
				sha: event.sha
			}, {
				id: event.appID
			});
		break;
	}
});
