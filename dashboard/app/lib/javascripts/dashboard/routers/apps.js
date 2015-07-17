import Router from 'marbles/router';
import { extend, assertEqual } from 'marbles/utils';
import { pathWithParams } from 'marbles/history';
import QueryParams from 'marbles/query_params';
import State from 'marbles/state';
import Dispatcher from '../dispatcher';
import Config from '../config';
import GithubPullsStore from '../stores/github-pulls';
import GithubCommitsStore from '../stores/github-commits';
import GithubBranchesStore from '../stores/github-branches';
import AppsComponent from '../views/apps';
import AppEnvComponent from '../views/app-env';
import AppDeleteComponent from '../views/app-delete';
import NewAppRouteComponent from '../views/app-route-new';
import AppRouteDeleteComponent from '../views/app-route-delete';
import AppDeployCommitComponent from '../views/app-deploy-commit';
import AppLogsComponent from '../views/app-logs';

var AppsRouter = Router.createClass({
	routes: [
		{ path: "apps", handler: "apps" },
		{ path: "apps/:id", handler: "app", paramChangeScrollReset: false, app: true },
		{ path: "apps/:id/env", handler: "appEnv", secondary: true, app: true },
		{ path: "apps/:id/logs", handler: "appLogs", secondary: true, app: true },
		{ path: "apps/:id/delete", handler: "appDelete", secondary: true, app: true },
		{ path: "apps/:id/routes/new", handler: "newAppRoute", secondary: true, app: true },
		{ path: "apps/:id/routes/:type/:route/delete", handler: "appRouteDelete", secondary: true, app: true },
		{ path: "apps/:id/deploy/:owner/:repo/:branch/:sha", handler: "appDeployCommit", secondary: true, app: true }
	],

	mixins: [State],

	willInitialize: function () {
		this.dispatcherIndex = Dispatcher.register(this.handleEvent.bind(this));
		this.state = {};
		this.__changeListeners = []; // always empty
	},

	beforeHandlerUnlaod: function (event) {
		// prevent commits/branches/pulls stores from expiring
		// when switching between source history tabs on the app page
		// and allow them to expire when navigating away
		var view = this.context.primaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.App") {
			var app = view.state.app;
			var appMeta = app ? app.meta : null;
			if (app && appMeta) {
				if (event.nextHandler.router === this) {
					if (view.props.selectedTab !== event.nextParams[0].shtab) {
						if (view.props.selectedTab === "pulls") {
							GithubPullsStore.expectChangeListener({
								ownerLogin: appMeta.user_login,
								repoName: appMeta.repo_name
							});
						} else if (event.nextParams[0].shtab === "pulls") {
							GithubCommitsStore.expectChangeListener({
								ownerLogin: appMeta.user_login,
								repoName: appMeta.repo_name,
								branch: view.props.selectedBranchName || appMeta.ref
							});
							GithubBranchesStore.expectChangeListener({
								ownerLogin: appMeta.user_login,
								repoName: appMeta.repo_name
							});
						}
					}
				} else {
					GithubPullsStore.unexpectChangeListener({
						ownerLogin: appMeta.user_login,
						repoName: appMeta.repo_name
					});
					GithubCommitsStore.unexpectChangeListener({
						ownerLogin: appMeta.user_login,
						repoName: appMeta.repo_name,
						branch: view.props.selectedBranchName || appMeta.ref
					});
					GithubBranchesStore.unexpectChangeListener({
						ownerLogin: appMeta.user_login,
						repoName: appMeta.repo_name
					});
				}
			}
		}
	},

	apps: function (params) {
		var view = this.context.primaryView;
		var props = this.__getAppsProps(params);
		if (view && view.isMounted() && view.constructor.displayName === "Views.Apps") {
			view.setProps(props);
		} else {
			this.context.primaryView = view = React.render(React.createElement(
				AppsComponent, props), this.context.el);
		}
	},

	__getAppsProps: function (params) {
		var appProps = this.__getAppProps(params);
		var showSystemApps = params[0].system === "true";
		var defaultRouteDomain = Config.default_route_domain;
		var getAppPath = function (appId) {
			var __params = extend({}, params[0]);
			delete __params.id;
			return this.__getAppPath(appId, __params, "");
		}.bind(this);
		return {
			showSystemApps: showSystemApps,
			defaultRouteDomain: defaultRouteDomain,
			appProps: appProps,
			appsListProps: {
				selectedAppId: appProps.appId,
				getAppPath: getAppPath,
				defaultRouteDomain: defaultRouteDomain,
				showSystemApps: showSystemApps,
			},
			appsListHeaderProps: {
				githubAuthed: !!Config.githubClient
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
				var __params = QueryParams.replaceParams.apply(null, [[extend({}, params)]].concat(subpathParams || []));
				return this.__getAppPath(params.id, __params[0], subpath);
			}.bind(this),
			getClusterPath: this.__getClusterPath.bind(this, params.id)
		};
	},

	appEnv: function (params) {
		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			AppEnvComponent,
			{
				appId: params.id,
				onHide: function () {
					this.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appLogs: function (params) {
		this.context.secondaryView = React.render(React.createElement(
			AppLogsComponent,
			this.__getAppLogsProps(params)),
			this.context.secondaryEl
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
				this.history.navigate(this.__getAppPath(params.id, params));
			}.bind(this)
		};
	},

	appDelete: function (params) {
		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			AppDeleteComponent,
			{
				appId: params.id,
				onHide: function () {
					this.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	newAppRoute: function (params) {
		this.setState({
			creatingRoute: true
		});

		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			NewAppRouteComponent,
			{
				key: Date.now(),
				appId: params.id,
				onHide: function () {
					this.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appRouteDelete: function (params) {
		this.setState({
			deletingRoute: true
		});

		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			AppRouteDeleteComponent,
			{
				key: params.id + params.route,
				appId: params.id,
				routeId: params.route,
				routeType: params.type,
				domain: params.domain,
				onHide: function () {
					var path = this.__getAppPath(params.id, QueryParams.replaceParams([extend({}, params)], {route: null, domain:null})[0]);
					this.history.navigate(path);
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appDeployCommit: function (params) {
		this.setState({
			deployCommitView: true
		});

		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			AppDeployCommitComponent,
			{
				appId: params.id,
				ownerLogin: params.owner,
				repoName: params.repo,
				branchName: params.branch,
				sha: params.sha,
				onHide: function () {
					var path = this.__getAppPath(params.id, QueryParams.replaceParams([extend({}, params)], {owner: null, repo: null, branch: null, sha: null})[0]);
					this.history.navigate(path);
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	handleEvent: function (event) {
		switch (event.name) {
			case 'handler:before':
				// reset state between routes
				this.state = {};
				if (event.handler.opts.app === true) {
					this.state.appID = event.params[0].id;
				}
			break;

			case 'APP_DELETED':
				if (this.state.appID === event.app) {
					this.history.navigate("");
				}
			break;

			case 'CREATE_APP_ROUTE':
				if (this.state.creatingRoute === true) {
					this.setState({
						routeDomain: event.data.domain
					});
				}
			break;

			case 'ROUTE':
				if (this.state.creatingRoute === true && event.data.domain === this.state.routeDomain) {
					this.__navigateToApp(event.app);
				}
			break;

			case 'DELETE_APP_ROUTE':
				if (this.state.deletingRoute === true) {
					this.setState({
						routeID: event.routeID
					});
				}
			break;

			case 'DELETE_APP_ROUTE_FAILED':
				if (this.state.deletingRoute === true && event.routeID === this.state.routeID && event.status === 404) {
					this.__navigateToApp(event.appID, {route: null, domain: null});
				}
			break;

			case 'ROUTE_DELETED':
				if (this.state.deletingRoute === true && event.data.id === this.state.routeID) {
					this.__navigateToApp(event.app, {route: null, domain: null});
				}
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

			case "GITHUB_PULL:MERGED":
				this.__handleGithubPullMerged(event);
			break;
		}
	},

	__handleCommitSelected: function (event) {
		var view = this.context.primaryView, appView;
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
				appProps: extend({}, view.props.appProps, {
					selectedSha: event.sha
				})
			});
		}
	},

	__handleBranchSelected: function (event) {
		var view = this.context.primaryView, appView;
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
				appProps: extend({}, view.props.appProps, {
					selectedBranchName: event.branchName
				})
			});
		}
	},

	__handleConfirmDeployCommit: function (event) {
		var appID = event.storeId ? event.storeId.appId : null;
		if (this.state.appID === appID) {
			this.history.navigate(this.__getAppPath(appID, {
				owner: event.ownerLogin,
				repo: event.repoName,
				branch: event.branchName,
				sha: event.sha
			}, "/deploy/:owner/:repo/:branch/:sha"));
		}
	},

	__handleGithubPullMerged: function (event) {
		var view = this.context.primaryView;
		var base = event.pull.base;
		if (view && view.isMounted() && view.constructor.displayName === "Views.Apps" && view.props.appProps.appId) {
			this.history.navigate(this.__getAppPath(view.props.appProps.appId, {
				owner: base.ownerLogin,
				repo: base.name,
				branch: base.ref,
				sha: event.mergeCommitSha
			}, "/deploy/:owner/:repo/:branch/:sha"));
		}
	},

	__getAppPath: function (appId, __params, subPath) {
		var params = QueryParams.deserializeParams(this.history.path.split("?")[1] || "");
		params = QueryParams.replaceParams(params, extend({id: appId}, __params));
		subPath = subPath || "";
		return pathWithParams("/apps/:id" + subPath, params);
	},

	__getClusterPath: function () {
		return "/apps";
	},

	__navigateToApp: function (appID, __params) {
		this.history.navigate(this.__getAppPath(appID, __params));
	}

});

export default AppsRouter;
