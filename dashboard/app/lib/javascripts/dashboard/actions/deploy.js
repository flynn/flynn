import { extend } from 'marbles/utils';
import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';

var createTaffyJob = function (taffyReleaseId, appID, appName, meta, env) {
	env = env || {};
	var args = ["/bin/taffy", appName, meta.clone_url, meta.branch, meta.rev];
	[{ arg: '--meta', data: meta }, { arg: '--env', data: env }].forEach(function (item) {
		Object.keys(item.data).forEach(function (k) {
			var v = item.data[k];
			args.push(item.arg);
			args.push(k +'='+ v);
		});
	});
	return Config.client.createTaffyJob({
		release: taffyReleaseId,
		release_env: true,
		args: args,
		meta: extend({}, meta, {
			app: appID
		})
	});
};

var deployFromGithub = function (meta, appData) {
	var client = Config.client;

	var appId, appName;
	var databaseEnv = {};

	function provisionResource (providerID) {
		return client.provisionResource(providerID, { apps: [appId] }).then(function (args) {
			var res = args[0];
			databaseEnv = extend({}, databaseEnv, res.env);
		});
	}

	function taffyDeploy () {
		return client.getTaffyRelease().then(function (args) {
			var res = args[0];
			var env = extend({}, appData.env, databaseEnv);
			return createTaffyJob(res.id, appId, appName, meta, env);
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
			}).catch(function (args) {
				Dispatcher.dispatch({
					name: 'APP_CREATE_FAILED',
					appName: appData.name,
					data: args[0]
				});
				return Promise.reject(args);
			});
		}
	}

	return findOrCreateApp().then(function (args) {
		var res = args[0];
		appId = res.id;
		appName = res.name;
		if ((appData.providerIDs || []).length > 0) {
			return Promise.all(appData.providerIDs.map(function (providerID) {
				return provisionResource(providerID);
			})).then(taffyDeploy);
		} else {
			return taffyDeploy();
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
		Config.client.deployAppRelease(event.appID, event.releaseID, event.deployTimeout);
		break;
	}
});
