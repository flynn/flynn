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

var assertEqual = Marbles.Utils.assertEqual.bind(Marbles.Utils);

Dashboard.routers.Apps = Marbles.Router.createClass({
	displayName: "routers.apps",

	routes: [
		{ path: "apps", handler: "apps" },
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
			var app = view.state.app;
			var appMeta = app ? app.meta : null;
			if (app && appMeta) {
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

	apps: function (params) {
		var view = Dashboard.primaryView;
		var props = this.__getAppsProps(params);
		if (view && view.isMounted() && view.constructor.displayName === "Views.Apps") {
			view.setProps(props);
		} else {
			Dashboard.primaryView = view = React.render(React.createElement(
				Dashboard.Views.Apps, props), Dashboard.el);
		}
	},

	__getAppsProps: function (params) {
		var appProps = this.__getAppProps(params);
		var showProtected = params[0].protected === "true";
		var defaultRouteDomain = Dashboard.config.default_route_domain;
		var getAppPath = function (appId) {
			var __params = Marbles.Utils.extend({}, params[0]);
			delete __params.id;
			return this.__getAppPath(appId, __params, "");
		}.bind(this);
		return {
			showProtected: showProtected,
			defaultRouteDomain: defaultRouteDomain,
			appProps: appProps,
			appsListProps: {
				selectedAppId: appProps.appId,
				getAppPath: getAppPath,
				defaultRouteDomain: defaultRouteDomain,
				showProtected: showProtected,
			},
			appsListHeaderProps: {
				githubAuthed: !!Dashboard.githubClient
			}
		};
	},

	app: function (params) {
		this.apps(params);
	},

	__getAppProps: function (params) {
		params = params[0];
		return {
			appId: params.id,
			selectedTab: params.shtab || null,
			getAppPath: function (subpath, subpathParams) {
				var __params = Marbles.QueryParams.replaceParams.apply(null, [[Marbles.Utils.extend({}, params)]].concat(subpathParams || []));
				return this.__getAppPath(params.id, __params[0], subpath);
			}.bind(this),
			getClusterPath: this.__getClusterPath.bind(this, params.id)
		};
	},

	appEnv: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.render(React.createElement(
			Dashboard.Views.AppEnv,
			{
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
		Dashboard.secondaryView = React.render(React.createElement(
			Dashboard.Views.AppLogs,
			this.__getAppLogsProps(params)),
			Dashboard.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	__getAppLogsProps: function (params) {
		params = params[0];
		return {
			taffyJobsStoreId: null,
			appId: params.id,
			onHide: function () {
				Marbles.history.navigate(this.__getAppPath(params.id, params));
			}.bind(this)
		};
	},

	appDelete: function (params) {
		params = params[0];

		Dashboard.secondaryView = React.render(React.createElement(
			Dashboard.Views.AppDelete,
			{
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

		Dashboard.secondaryView = React.render(React.createElement(
			Dashboard.Views.NewAppRoute,
			{
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

		Dashboard.secondaryView = React.render(React.createElement(
			Dashboard.Views.AppRouteDelete,
			{
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

		Dashboard.secondaryView = React.render(React.createElement(
			Dashboard.Views.AppDeployCommit,
			{
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
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppEnv" && assertEqual(view.props.appId, event.appId) && view.state.isSaving) {
			this.__navigateToApp(event);
		}
	},

	__handleAppDeleted: function (event) {
		// exit app delete view when successfully deleted
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppDelete" && assertEqual(view.props.appId, event.appId) && view.state.isDeleting) {
			Marbles.history.navigate("");
		}
	},

	__handleAppRouteCreated: function (event) {
		// exit app rotue delete view when successfully deleted
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.NewAppRoute" && assertEqual(view.props.appId, event.appId) && view.state.isCreating) {
			this.__navigateToApp(event);
		}
	},

	__handleAppRouteCreateFailure: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppRouteDelete" && assertEqual(view.props.appId, event.appId) && view.state.isDeleting) {
			view.setProps({
				errorMsg: event.errorMsg
			});
		}
	},

	__handleAppRouteDeleted: function (event) {
		// exit app rotue delete view when successfully deleted
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppRouteDelete" && assertEqual(view.props.appId, event.appId) && view.props.routeId === event.routeId && view.state.isDeleting) {
			this.__navigateToApp(event, {route: null, domain: null});
		}
	},

	__handleAppRouteDeleteFailure: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppRouteDelete" && assertEqual(view.props.appId, event.appId) && view.props.routeId === event.routeId && view.state.isDeleting) {
			view.setProps({
				errorMsg: event.errorMsg
			});
		}
	},

	__handleCommitSelected: function (event) {
		var view = Dashboard.primaryView, appView;
		if (view.refs && view.refs.appComponent) {
			appView = view.refs.appComponent;
		} else {
			return;
		}
		var storeId = event.storeId;
		var app = appView.state ? appView.state.app : null;
		var meta = app ? app.meta : null;
		if (storeId && meta && view && view.isMounted() && view.constructor.displayName === "Views.Apps" && meta.user_login === storeId.ownerLogin && meta.repo_name === storeId.repoName) {
			view.setProps({
				appProps: Marbles.Utils.extend({}, view.props.appProps, {
					selectedSha: event.sha
				})
			});
		}
	},

	__handleBranchSelected: function (event) {
		var view = Dashboard.primaryView, appView;
		if (view && view.refs && view.refs.appComponent) {
			appView = view.refs.appComponent;
		} else {
			return;
		}
		var storeId = event.storeId;
		var app = appView.state ? appView.state.app : null;
		var meta = app ? app.meta : null;
		if (storeId && meta && view && view.isMounted() && view.constructor.displayName === "Views.Apps" && meta.user_login === storeId.ownerLogin && meta.repo_name === storeId.repoName) {
			view.setProps({
				appProps: Marbles.Utils.extend({}, view.props.appProps, {
					selectedBranchName: event.branchName
				})
			});
		}
	},

	__handleConfirmDeployCommit: function (event) {
		var view = Dashboard.primaryView;
		var appId = event.storeId ? event.storeId.appId : null;
		if (view && view.isMounted() && view.constructor.displayName === "Views.Apps" && view.props.appProps.appId === appId) {
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
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppDeployCommit" && assertEqual(view.props.appId, event.appId)) {
			view.setProps({
				job: event.job
			});
		}
	},

	__handleDeployFailure: function (event) {
		var view = Dashboard.secondaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.AppDeployCommit" && assertEqual(view.props.appId, event.appId)) {
			view.setProps({
				errorMsg: event.errorMsg
			});
		}
	},

	__handleGithubPullMerged: function (event) {
		var view = Dashboard.primaryView;
		var base = event.pull.base;
		if (view && view.isMounted() && view.constructor.displayName === "Views.Apps" && view.props.appProps.appId) {
			Marbles.history.navigate(this.__getAppPath(view.props.appProps.appId, {
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

	__getClusterPath: function () {
		return "/apps";
	},

	__navigateToApp: function (event, __params) {
		Marbles.history.navigate(this.__getAppPath(event.appId, __params));
	}

});

})();
