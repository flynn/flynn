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
import AppResourceProvisioner from '../views/app-resource-provisioner';
import ResourceDeleteComponent from '../views/resource-delete';
import NewAppRouteComponent from '../views/app-route-new';
import AppRouteDeleteComponent from '../views/app-route-delete';
import AppDeployCommitComponent from '../views/app-deploy-commit';
import AppDeployEventComponent from '../views/app-deploy-event';
import AppLogsComponent from '../views/app-logs';
import DeployAppEventStore from '../stores/deploy-app-event';

var AppsRouter = Router.createClass({
	routes: [
		{ path: "apps", handler: "apps" },
		{ path: "apps/:id", handler: "app", paramChangeScrollReset: false, app: true },
		{ path: "apps/:id/env", handler: "appEnv", secondary: true, app: true },
		{ path: "apps/:id/logs", handler: "appLogs", app: true },
		{ path: "apps/:id/delete", handler: "appDelete", secondary: true, app: true },
		{ path: "apps/:id/resources/new", handler: "newAppResource", secondary: true, app: true },
		{ path: "apps/:id/providers/:providerID/resources/:resourceID/delete", handler: "appResourceDelete", secondary: true, app: true },
		{ path: "apps/:id/routes/new", handler: "newAppRoute", secondary: true, app: true },
		{ path: "apps/:id/routes/:type/:route/delete", handler: "appRouteDelete", secondary: true, app: true },
		{ path: "apps/:id/deploy/:owner/:repo/:branch/:sha", handler: "appDeployCommit", secondary: true, app: true },
		{ path: "apps/:id/deploy/:event", handler: "appDeployEvent", secondary: true, app: true }
	],

	mixins: [State],

	willInitialize: function () {
		this.dispatcherIndex = Dispatcher.register(this.handleEvent.bind(this));
		this.state = {};
		this.__changeListeners = []; // always empty
	},

	beforeHandlerUnload: function (event) {
		// prevent commits/branches/pulls stores from expiring
		// when switching between source history tabs on the app page
		// and allow them to expire when navigating away
		var view = this.context.primaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.App") {
			var release = view.state.release;
			var meta = release ? release.meta : null;
			if (release && meta) {
				if (event.nextHandler.router === this) {
					if (view.props.selectedTab !== event.nextParams[0].shtab) {
						if (view.props.selectedTab === "pulls") {
							GithubPullsStore.expectChangeListener({
								ownerLogin: meta.github_user,
								repoName: meta.github_repo
							});
						} else if (event.nextParams[0].shtab === "pulls") {
							GithubCommitsStore.expectChangeListener({
								ownerLogin: meta.github_user,
								repoName: meta.github_repo,
								branch: view.props.selectedBranchName || meta.branch
							});
							GithubBranchesStore.expectChangeListener({
								ownerLogin: meta.github_user,
								repoName: meta.github_repo
							});
						}
					}
				} else {
					GithubPullsStore.unexpectChangeListener({
						ownerLogin: meta.github_user,
						repoName: meta.github_repo
					});
					GithubCommitsStore.unexpectChangeListener({
						ownerLogin: meta.github_user,
						repoName: meta.github_repo,
						branch: view.props.selectedBranchName || meta.branch
					});
					GithubBranchesStore.unexpectChangeListener({
						ownerLogin: meta.github_user,
						repoName: meta.github_repo
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
				showSystemApps: showSystemApps
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
		this.context.primaryView = React.render(React.createElement(
			AppLogsComponent,
			this.__getAppLogsProps(params)),
			this.context.el
		);
	},

	__getAppLogsProps: function (params) {
		params = params[0];
		return {
			taffyJobsStoreId: null,
			appId: params.id,
			lines: parseInt(params.lines || '') || 200,
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

	newAppResource: function (params) {
		this.setState({
			creatingResource: true
		});

		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			AppResourceProvisioner,
			{
				key: Date.now(),
				appID: params.id,
				onHide: function () {
					this.history.navigate(this.__getAppPath(params.id, params));
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appResourceDelete: function (params) {
		params = params[0];

		this.setState({
			deletingResource: true,
			appID: params.id,
			providerID: params.providerID,
			resourceID: params.resourceID
		});

		this.context.secondaryView = React.render(React.createElement(
			ResourceDeleteComponent,
			{
				key: Date.now(),
				appID: params.id,
				providerID: params.providerID,
				resourceID: params.resourceID,
				onHide: function () {
					this.history.navigate(this.__getAppPath(params.id, extend({}, params, {providerID: null, resourceID: null})));
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
					var path = this.__getAppPath(params.id, QueryParams.replaceParams([extend({}, params)], {route: null, domain: null, type: null})[0]);
					this.history.navigate(path);
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render app view in background
		this.app.apply(this, arguments);
	},

	appDeployCommit: function (params) {
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

	appDeployEvent: function (params) {
		params = params[0];

		var eventID = parseInt(params.event, 10);

		this.setState({
			deployingEvent: true,
			eventID: eventID
		});

		this.context.secondaryView = React.render(React.createElement(
			AppDeployEventComponent,
			{
				key: params.id + params.event,
				appID: params.id,
				eventID: eventID,
				onHide: function () {
					var path = this.__getAppPath(params.id, QueryParams.replaceParams([extend({}, params)], {event: null})[0]);
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

		case 'RESOURCE_DELETED':
		case 'RESOURCE_APP_DELETED':
			if (this.state.deletingResource && event.app === this.state.appID && event.object_id === this.state.resourceID) {
				this.__navigateToApp(this.state.appID);
			}
			break;

		case 'APP_PROVISION_RESOURCES':
			if (this.state.creatingResource && event.appID === this.state.appID) {
				this.setState({
					providerIDs: event.providerIDs
				});
			}
			break;

		case 'RESOURCE':
			if (this.state.creatingResource && event.app === this.state.appID) {
				this.setState({
					providerIDs: (this.state.providerIDs || []).filter(function (providerID) {
						return providerID !== event.data.provider;
					})
				});
			}
			break;

		case 'DEPLOYMENT':
			if (this.state.creatingResource && event.app === this.state.appID) {
				if (event.data.status === 'complete') {
					this.__navigateToApp(this.state.appID);
				} else if (event.data.status === 'failed') {
					window.console.error('Failed to deploy app release.');
				}
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

		case 'CONFIRM_DEPLOY_APP_EVENT':
			if (this.state.appID === event.appID) {
				this.history.navigate(this.__getAppPath(event.appID, {event: event.eventID}, '/deploy/:event'));
			}
			break;

		case 'SCALE':
			this.__handleScaleEvent(event);
			break;

		case 'DEPLOYMENT':
			this.__handleDeploymentEvent(event);
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

		case "GITHUB_AUTH_CHANGE":
			if (this.context.waitingForNav) {
				return;
			}
			this.history.navigate(this.history.path, { force: true, replace: true });
			break;
		}
	},

	__handleScaleEvent: function (event) {
		if (this.state.deployingEvent !== true || event.app !== this.state.appID) {
			return;
		}
		var processes = DeployAppEventStore.getState({eventID: this.state.eventID}).processes;
		if (assertEqual(processes, event.data.processes)) {
			this.history.navigate(this.__getAppPath(this.state.appID));
		}
	},

	__handleDeploymentEvent: function (event) {
		var deployState = DeployAppEventStore.getState({eventID: this.state.eventID});
		if (this.state.deployingEvent !== true || event.app !== this.state.appID) {
			return;
		}
		if (deployState.event && deployState.event.app === event.app && deployState.event.object_id === event.object_id && event.status !== 'failed') {
			this.history.navigate(this.__getAppPath(this.state.appID));
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
		var release = appView.state ? appView.state.release : null;
		var meta = release ? release.meta : null;
		if (storeId && meta && view && view.isMounted() && view.constructor.displayName === "Views.Apps" && meta.github_user === storeId.ownerLogin && meta.github_repo === storeId.repoName) {
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
		var release = appView.state ? appView.state.release : null;
		var meta = release ? release.meta : null;
		if (storeId && meta && view && view.isMounted() && view.constructor.displayName === "Views.Apps" && meta.github_user === storeId.ownerLogin && meta.github_repo === storeId.repoName) {
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
