//= require ../dispatcher
//= require ../views/app
//= require ../views/app-env
//= require ../views/app-logs
//= require ../views/app-delete
//= require ../views/app-route-new
//= require ../views/app-route-delete
//= require ../views/app-deploy-commit

(function () {

"use strict";

Dashboard.routers.Apps = Marbles.Router.createClass({
	displayName: "routers.apps",

	routes: [
		{ path: "apps/:id", handler: "app", paramChangeScrollReset: false },
		{ path: "apps/:id/env", handler: "appEnv", secondary: true },
		{ path: "apps/:id/logs", handler: "appLogs", secondary: true },
		{ path: "apps/:id/delete", handler: "appDelete", secondary: true },
		{ path: "apps/:id/routes/new", handler: "newAppRoute", secondary: true },
		{ path: "apps/:id/routes/:route/delete", handler: "appRouteDelete", secondary: true },
		{ path: "apps/:id/deploy/:owner/:repo/:branch/:sha", handler: "appDeployCommit", secondary: true }
	],

	beforeHandlerUnlaod: function (event) {
		// prevent commits/branches/pulls stores from expiring
		// when switching between source history tabs on the app page
		// and allow them to expire when navigating away
		var view = Dashboard.primaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.App") {
			if (view.state.app) {
				var appMeta = view.state.app.meta;
				if (event.nextHandler.router === this) {
					if (view.props.selectedTab !== event.nextParams[0].shtab) {
						if (view.props.selectedTab === "pulls") {
							Dashboard.Stores.GithubPulls.expectChangeListener({
								ownerLogin: appMeta.user_login,
								repoName: appMeta.repo_name
							});
						} else if (event.nextParams[0].shtab === "pulls") {
							Dashboard.Stores.GithubCommits.expectChangeListener({
								ownerLogin: appMeta.user_login,
								repoName: appMeta.repo_name,
								branch: view.props.selectedBranchName || appMeta.ref
							});
							Dashboard.Stores.GithubBranches.expectChangeListener({
								ownerLogin: appMeta.user_login,
								repoName: appMeta.repo_name
							});
						}
					}
				} else {
					Dashboard.Stores.GithubPulls.unexpectChangeListener({
						ownerLogin: appMeta.user_login,
						repoName: appMeta.repo_name
					});
					Dashboard.Stores.GithubCommits.unexpectChangeListener({
						ownerLogin: appMeta.user_login,
						repoName: appMeta.repo_name,
						branch: view.props.selectedBranchName || appMeta.ref
					});
					Dashboard.Stores.GithubBranches.unexpectChangeListener({
						ownerLogin: appMeta.user_login,
						repoName: appMeta.repo_name
					});
				}
			}
		}
	},

	app: function (params) {
		params = params[0];
		var view = Dashboard.primaryView;
		var props = {
			appId: params.id,
			selectedTab: params.shtab || null,
			getAppPath: function (subpath, subpathParams) {
				var __params = Marbles.QueryParams.replaceParams.apply(null, [[Marbles.Utils.extend({}, params)]].concat(subpathParams || []));
				return this.__getAppPath(params.id, __params[0], subpath);
			}.bind(this)
		};
		if (view && view.isMounted() && view.constructor.displayName === "Views.App") {
			view.setProps(props);
		} else {
			Dashboard.primaryView = view = React.renderComponent(
				Dashboard.Views.App(props),
				Dashboard.el);
			}
	},

	appEnv: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.renderComponent(
			Dashboard.Views.AppEnv({
				appId: params.id,
				onHide: function () {
					Marbles.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			Dashboard.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appLogs: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.renderComponent(
			Dashboard.Views.AppLogs({
				appId: params.id,
				onHide: function () {
					Marbles.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			Dashboard.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appDelete: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.renderComponent(
			Dashboard.Views.AppDelete({
				appId: params.id,
				onHide: function () {
					Marbles.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			Dashboard.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	newAppRoute: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.renderComponent(
			Dashboard.Views.NewAppRoute({
				appId: params.id,
				onHide: function () {
					Marbles.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			Dashboard.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appRouteDelete: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.renderComponent(
			Dashboard.Views.AppRouteDelete({
				appId: params.id,
				routeId: params.route,
				domain: params.domain,
				onHide: function () {
					var path = this.__getAppPath(params.id, Marbles.QueryParams.replaceParams([Marbles.Utils.extend({}, params)], {route: null, domain:null})[0]);
					Marbles.history.navigate(path);
				}.bind(this)
			}),
			Dashboard.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appDeployCommit: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.renderComponent(
			Dashboard.Views.AppDeployCommit({
				appId: params.id,
				ownerLogin: params.owner,
				repoName: params.repo,
				branchName: params.branch,
				sha: params.sha,
				onHide: function () {
					var path = this.__getAppPath(params.id, Marbles.QueryParams.replaceParams([Marbles.Utils.extend({}, params)], {owner: null, repo: null, branch: null, sha: null})[0]);
					Marbles.history.navigate(path);
				}.bind(this)
			}),
			Dashboard.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	handleEvent: function (event) {
		switch (event.name) {
			case "APP:RELEASE_CREATED":
				this.__handleReleaseCreated(event);
			break;

			case "APP:DELETED":
				this.__handleAppDeleted(event);
			break;

			case "APP_ROUTES:CREATED":
				this.__handleAppRouteCreated(event);
			break;

			case "APP_ROUTES:CREATE_FAILED":
				this.__handleAppRouteCreateFailure(event);
			break;

			case "APP_ROUTES:DELETED":
				this.__handleAppRouteDeleted(event);
			break;

			case "APP_ROUTES:DELETE_FAILED":
				this.__handleAppRouteDeleteFailure(event);
			break;

			case "GITHUB_BRANCH_SELECTOR:BRANCH_SELECTED":
				this.__handleBranchSelected(event);
			break;

			case "GITHUB_COMMITS:COMMIT_SELECTED":
				this.__handleCommitSelected(event);
			break;

			case "APP_SOURCE_HISTORY:CONFIRM_DEPLOY_COMMIT":
				this.__handleConfirmDeployCommit(event);
			break;

			case "APP:JOB_CREATED":
				this.__handleJobCreated(event);
			break;

			case "APP:DEPLOY_FAILED":
				this.__handleDeployFailure(event);
			break;

			case "GITHUB_PULL:MERGED":
				this.__handleGithubPullMerged(event);
			break;
		}
	},

	__handleReleaseCreated: function (event) {
		// exit app env view when successfully saved
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppEnv" && view.props.appId === event.appId && view.state.isSaving) {
			this.__navigateToApp(event);
		}
	},

	__handleAppDeleted: function (event) {
		// exit app delete view when successfully deleted
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppDelete" && view.props.appId === event.appId && view.state.isDeleting) {
			Marbles.history.navigate("");
		}
	},

	__handleAppRouteCreated: function (event) {
		// exit app rotue delete view when successfully deleted
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.NewAppRoute" && view.props.appId === event.appId && view.state.isCreating) {
			this.__navigateToApp(event);
		}
	},

	__handleAppRouteCreateFailure: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppRouteDelete" && view.props.appId === event.appId && view.state.isDeleting) {
			view.setProps({
				errorMsg: event.errorMsg
			});
		}
	},

	__handleAppRouteDeleted: function (event) {
		// exit app rotue delete view when successfully deleted
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppRouteDelete" && view.props.appId === event.appId && view.props.routeId === event.routeId && view.state.isDeleting) {
			this.__navigateToApp(event, {route: null, domain: null});
		}
	},

	__handleAppRouteDeleteFailure: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppRouteDelete" && view.props.appId === event.appId && view.props.routeId === event.routeId && view.state.isDeleting) {
			view.setProps({
				errorMsg: event.errorMsg
			});
		}
	},

	__handleCommitSelected: function (event) {
		var view = Dashboard.primaryView;
		var storeId = event.storeId;
		var app = view.state ? view.state.app : null;
		var meta = app ? app.meta : null;
		if (storeId && meta && view && view.isMounted() && view.constructor.displayName === "Views.App" && storeId && meta.user_login === storeId.ownerLogin && meta.repo_name === storeId.repoName) {
			view.setProps({
				selectedSha: event.sha
			});
		}
	},

	__handleBranchSelected: function (event) {
		var view = Dashboard.primaryView;
		var storeId = event.storeId;
		var app = view.state ? view.state.app : null;
		var meta = app ? app.meta : null;
		if (storeId && meta && view && view.isMounted() && view.constructor.displayName === "Views.App" && storeId && meta.user_login === storeId.ownerLogin && meta.repo_name === storeId.repoName) {
			view.setProps({
				selectedBranchName: event.branchName
			});
		}
	},

	__handleConfirmDeployCommit: function (event) {
		var view = Dashboard.primaryView;
		var appId = event.storeId ? event.storeId.appId : null;
		if (view && view.isMounted() && view.constructor.displayName === "Views.App" && view.props.appId === appId) {
			Marbles.history.navigate(this.__getAppPath(appId, {
				owner: event.ownerLogin,
				repo: event.repoName,
				branch: event.branchName,
				sha: event.sha
			}, "/deploy/:owner/:repo/:branch/:sha"));
		}
	},

	__handleJobCreated: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppDeployCommit" && view.props.appId === event.appId) {
			view.setProps({
				job: event.job
			});
		}
	},

	__handleDeployFailure: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppDeployCommit" && view.props.appId === event.appId) {
			view.setProps({
				errorMsg: event.errorMsg
			});
		}
	},

	__handleGithubPullMerged: function (event) {
		var view = Dashboard.primaryView;
		var base = event.pull.base;
		if (view && view.isMounted() && view.constructor.displayName === "Views.App") {
			Marbles.history.navigate(this.__getAppPath(view.props.appId, {
				owner: base.ownerLogin,
				repo: base.name,
				branch: base.ref,
				sha: event.mergeCommitSha
			}, "/deploy/:owner/:repo/:branch/:sha"));
		}
	},

	__getAppPath: function (appId, __params, subPath) {
		var params = Marbles.QueryParams.deserializeParams(Marbles.history.path.split("?")[1] || "");
		params = Marbles.QueryParams.replaceParams(params, Marbles.Utils.extend({id: appId}, __params));
		subPath = subPath || "";
		return Marbles.history.pathWithParams("/apps/:id" + subPath, params);
	},

	__navigateToApp: function (event, __params) {
		Marbles.history.navigate(this.__getAppPath(event.appId, __params));
	}

});

})();
