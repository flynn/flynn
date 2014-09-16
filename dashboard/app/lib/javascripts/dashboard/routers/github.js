//= require ../views/github-auth
//= require ../views/github
//= require ../views/github-deploy

(function () {

"use strict";

Dashboard.routers.Github = Marbles.Router.createClass({
	displayName: "routers.github",

	routes: [
		{ path: "github/auth", handler: "auth", githubAuth: false },
		{ path: "github", handler: "github" },
		{ path: "github/deploy", handler: "deploy", secondary: true },
	],

	beforeHandler: function (event) {
		// ensure github authorization
		if ( !Dashboard.githubClient ) {
			if (event.handler.opts.githubAuth === false) {
				return;
			}
			event.abort();
			this.__redirectToGithub({replace: true});
		} else {
			var redirect = window.localStorage.getItem("github:path");
			window.localStorage.removeItem("github:path");
			if (redirect) {
				event.abort();
				Marbles.history.navigate(redirect, {replace: true, force: true});
			}
		}
	},

	auth: function () {
		var props = {};
		var view = Dashboard.primaryView;
		if (view && view.constructor.displayName === "Views.GithubAuth" && view.isMounted()) {
			view.setProps(props);
		} else {
			view = Dashboard.primaryView = React.renderComponent(
				Dashboard.Views.GithubAuth(props),
				Dashboard.el
			);
		}
	},

	github: function (params) {
		var selectedRepo;
		if (params[0].repo && params[0].owner) {
			selectedRepo = {
				ownerLogin: params[0].owner,
				name: params[0].repo
			};
		}
		var props = {
			selectedSource: params[0].org || null,
			selectedType: params[0].type || null,
			selectedRepo: selectedRepo || null,
			selectedRepoPanel: params[0].repo_panel || null,
			selectedBranchName: params[0].branch || null,
			getClusterPath: this.__getClusterPath.bind(this, params)
		};
		var view = Dashboard.primaryView;
		if (view && view.constructor.displayName === "Views.Github" && view.isMounted()) {
			view.setProps(props);
		} else {
			view = Dashboard.primaryView = React.renderComponent(
				Dashboard.Views.Github(props),
				Dashboard.el);
			}
	},

	deploy: function (params) {
		params = params[0];
		var prevPath = Marbles.history.prevPath;
		if (prevPath === Marbles.history.path) {
			prevPath = null;
		}
		var githubParams = [{
			owner: params.base_owner || params.owner,
			repo: params.base_repo || params.repo
		}];
		if (params.pull) {
			githubParams[0].repo_panel = "pulls";
		}
		var prevPathParts;
		if (prevPath) {
			prevPathParts = prevPath.split("?");
			if (prevPathParts[0] === "github") {
				githubParams = Marbles.QueryParams.deserializeParams(prevPathParts[1]);
			}
		}
		var githubPath = Marbles.history.pathWithParams("/github", githubParams);

		var props = {
			onHide: function () {
				Marbles.history.navigate(prevPath || githubPath);
			},
			ownerLogin: params.owner,
			repoName: params.repo,
			branchName: params.branch,
			pullNumber: params.pull ? Number(params.pull) : null,
			sha: params.sha,
			dismissError: function () {
				view.setProps({ errorMsg: null });
			}
		};
		if (params.base_owner && params.base_repo) {
			props.baseOwner = params.base_owner;
			props.baseRepo = params.base_repo;
		}
		var view = Dashboard.secondaryView = React.renderComponent(
			Dashboard.Views.GithubDeploy(props),
			Dashboard.secondaryEl
		);

		if ( !prevPath ) {
			this.github(githubParams);
		}
	},

	__redirectToGithub: function (opts) {
		window.localStorage.setItem("github:path", Marbles.history.path);
		Marbles.history.navigate("/github/auth", opts || {});
	},

	handleEvent: function (event) {
		switch (event.name) {
			case "GITHUB_BRANCH_SELECTOR:BRANCH_SELECTED":
				this.__handleBranchSelected(event);
			break;

			case "GITHUB_COMMITS:LAUNCH_COMMIT":
				this.__handleLaunchCommit(event);
			break;

			case "GITHUB_PULLS:LAUNCH_PULL":
				this.__handleLaunchPull(event);
			break;

			case "APP:DATABASE_CREATED":
				this.__handleDatabaseCreated(event);
			break;

			case "APP:JOB_CREATED":
				this.__handleJobCreated(event);
			break;

			case "APP:CREATE_FAILED":
				this.__handleAppCreateFailed(event);
			break;

			case "GITHUB_AUTH_CHANGE":
				this.__handleGithubAuthChange(event.authenticated);
			break;
		}
	},

	__handleBranchSelected: function (event) {
		var path = Marbles.history.getPath();
		var pathParts = path.split("?");
		var handler = Marbles.history.getHandler(pathParts[0]);
		var params = [{}];
		if (handler.name === "github") {
			params = Marbles.QueryParams.deserializeParams(pathParts[1] || "");
		}
		if (params[0].repo === event.storeId.repoName && params[0].owner === event.storeId.ownerLogin) {
			params = Marbles.QueryParams.replaceParams(params, {
				branch: event.branchName
			});
			Marbles.history.navigate(Marbles.history.pathWithParams("/github", params));
		}
	},

	__handleLaunchCommit: function (event) {
		var storeId = event.storeId;
		var deployParams = {};
		deployParams.owner = storeId.ownerLogin;
		deployParams.repo = storeId.repoName;
		deployParams.sha = event.sha;
		deployParams.branch = storeId.branch;
		Marbles.history.navigate(Marbles.history.pathWithParams("/github/deploy", [deployParams]));
	},

	__handleLaunchPull: function (event) {
		var head = event.pull.head;
		var base = event.pull.base;
		var deployParams = {};
		deployParams.owner = head.ownerLogin;
		deployParams.repo = head.name;
		deployParams.base_owner = base.ownerLogin;
		deployParams.base_repo = base.name;
		deployParams.sha = head.sha;
		deployParams.branch = head.ref;
		deployParams.pull = event.pull.number;
		Marbles.history.navigate(Marbles.history.pathWithParams("/github/deploy", [deployParams]));
	},

	__handleDatabaseCreated: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.constructor.displayName === "Views.GithubDeploy" && view.isMounted() && view.state.name === event.appName) {
			view.setProps({
				env: event.env
			});
		}
	},

	__handleJobCreated: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.constructor.displayName === "Views.GithubDeploy" && view.isMounted() && view.state.name === event.appName) {
			view.setProps({
				appId: event.appId,
				job: event.job
			});
		}
	},

	__handleAppCreateFailed: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.constructor.displayName === "Views.GithubDeploy" && view.isMounted() && view.state.name === event.appName) {
			view.setProps({
				errorMsg: event.errorMsg
			});
		}
	},

	__handleGithubAuthChange: function (authenticated) {
		if ( !authenticated && Marbles.history.path.match(/^github/) ) {
			this.__redirectToGithub();
		}
	},

	__getClusterPath: function () {
		return "/";
	}
});

})();
