import { extend } from 'marbles/utils';
import History from 'marbles/history';
import QueryParams from 'marbles/query_params';
import MainRouter from './routers/main';
import ProvidersRouter from './routers/providers';
import AppsRouter from './routers/apps';
import GithubRouter from './routers/github';
import Dispatcher from './dispatcher';
import Config from './config';
import Client from './client';
import GithubClient from './github-client';
import ServiceUnavailableComponent from './views/service-unavailable';
import NavComponent from './views/nav';
/* eslint-disable no-unused-vars */
import Actions from './actions';
/* eslint-enable */

var Dashboard = function () {
	var history = this.history = new History();

	this.dispatcherIndex = Dispatcher.register(this.__handleEvent.bind(this));

	history.register(new MainRouter({ context: this }));
	history.register(new ProvidersRouter({ context: this }));
	history.register(new AppsRouter({ context: this }));
	history.register(new GithubRouter({ context: this }));
};
Dashboard.run = function () {
	var dashboard = new Dashboard();
	dashboard.run();
};
extend(Dashboard.prototype, {
	errCertNotInstalled: new Error("HTTPS certificate is not trusted."),
	errServiceUnavailable: new Error("Service is unavailable."),

	run: function () {
		Config.fetch().catch(
				function(){}); // suppress SERVICE_UNAVAILABLE error
	},

	ready: function () {
		var resolveWaitForNav;
		this.waitForNav = new Promise(function(resolve) {
			resolveWaitForNav = resolve;
		});

		var loadURL = function() {
			resolveWaitForNav();
			this.history.loadURL();
		}.bind(this);

		if ( this.history.started ) {
			throw new Error("history already started!");
		}

		// TODO(jvatic): Move these into ./config.js
		Config.setClient(new Client(Config.endpoints));
		if (Config.user && Config.user.auths.github) {
			Config.setGithubClient(new GithubClient(
				Config.user.auths.github.access_token,
				Config.github_api_url
			));
		}

		Config.history = this.history;

		this.navEl = document.getElementById("nav");
		this.el = document.getElementById("main");
		this.secondaryEl = document.getElementById("secondary");

		this.__secondary = false;

		this.history.start({
			root: (Config.PATH_PREFIX || '') + '/',
			dispatcher: Dispatcher,
			trigger: false
		});

		this.__setCurrentParams();
		if (Config.INSTALL_CERT) {
			this.__isCertInstalled().then(loadURL);
		} else {
			loadURL();
		}
	},

	__renderNavComponent: function () {
		this.nav = React.render(React.createElement(NavComponent, {
			authenticated: Config.authenticated
		}), this.navEl);
	},

	__isLoginPath: function (path) {
		path = path || this.history.path;
		if ( path === "" ) {
			return false;
		}
		return String(path).substr(0, 5) === 'login';
	},

	__redirectToLogin: function () {
		var redirectPath = this.history.path ? '?redirect='+ encodeURIComponent(this.history.path) : '';
		var loginParams = {};
		var currentParams = this.__currentParams[0];
		if (currentParams.token) {
			loginParams.token = currentParams.token;
		}
		this.history.navigate('login'+ redirectPath, {
			params: [loginParams]
		});
	},

	__catchInsecurePingResponse: function(httpsArgs) {
		var httpsXhr = httpsArgs[1],
			handleSuccess, handleError;

		handleSuccess = function (httpArgs) {
			var httpXhr = httpArgs[1];
			// https did not work but http did...something is wrong with the cert
			Dispatcher.handleAppEvent({
				name: "HTTPS_CERT_MISSING",
				status: httpXhr.status
			});
			return Promise.reject(this.errCertNotInstalled);
		}.bind(this);
		handleError = function (httpArgs) {
			if (!Array.isArray(httpArgs)) {
				return Promise.reject(httpArgs);
			}
			var httpXhr = httpArgs[1];

			if (httpXhr.status === 0) {
				// https is failing as well...service is unavailable
				Dispatcher.handleAppEvent({
					name: "SERVICE_UNAVAILABLE",
					status: httpXhr.status
				});
				return Promise.reject(this.errServiceUnavailable);
			}
			// https did not work but http did without a network error
			// => missing ssl exception for controller
			Dispatcher.handleAppEvent({
				name: "HTTPS_CERT_MISSING",
				status: httpXhr.status
			});
			return Promise.reject(this.errCertNotInstalled);
		}.bind(this);

		if (httpsXhr.status === 0) {
			// https is unavailable, let's see if http works
			return Config.client.ping("controller", "http").then(handleSuccess).catch(handleError);
		}
		// an error code other than 0
		Dispatcher.handleAppEvent({
			name: "SERVICE_UNAVAILABLE",
			status: httpsXhr.status
		});
		return Promise.reject(this.errServiceUnavailable);
	},

	__catchSecurePingResponse: function(args) {
		var xhr = args[1];
		if (xhr.status === 0) {
			// We were not able to access the controller due to a network error (ssl, timeout)
			// In order to understand what's happening, we have to switch to http.
			Dispatcher.handleAppEvent({
				name: "CONTROLLER_UNREACHABLE_FROM_HTTPS",
				status: xhr.status
			});
			return Promise.reject(new Error("CONTROLLER_UNREACHABLE_FROM_HTTPS"));
		}

		// an error code other than 0
		Dispatcher.handleAppEvent({
			name: "SERVICE_UNAVAILABLE",
			status: xhr.status
		});
		return Promise.reject(this.errServiceUnavailable);
	},

	__successPingResponse: function(args) {
		var xhr = args[1];
		if (xhr.status === 200) {
			window.location.href = window.location.href.replace("http:", "https:");
		}
	},

	__isCertInstalled: function() {
		if (window.location.protocol === "https:") {
			return Config.client.ping("controller", "https").catch(this.__catchSecurePingResponse.bind(this));
		} else {
			return Config.client.ping("controller", "https")
				.then(this.__successPingResponse.bind(this))
				.catch(this.__catchInsecurePingResponse.bind(this));
		}
	},

	__setCurrentParams: function () {
		this.__currentParams = QueryParams.deserializeParams(window.location.search);
	},

	__handleEvent: function (event) {
		if (event.source === "Marbles.History") {
			switch (event.name) {
			case "handler:before":
				this.__setCurrentParams();
				this.__handleHandlerBeforeEvent(event);
				break;

			case "handler:after":
				this.__handleHandlerAfterEvent(event);
				break;
			}
			return;
		}

		if (event.name === "AUTH_BTN_CLICK") {
			if (Config.authenticated) {
				Config.client.logout();
			} else if ( !this.__isLoginPath() ) {
				this.__redirectToLogin();
			}
		}

		if (event.source === "APP_EVENT") {
			this.__handleAppEvent(event);
		}
	},

	__handleAppEvent: function (event) {
		switch (event.name) {
		case "CONFIG_READY":
			this.__handleConfigReady();
			break;

		case "AUTH_CHANGE":
			this.__handleAuthChange(event.authenticated);
			break;

		case "GITHUB_AUTH_CHANGE":
			this.__handleGithubAuthChange(event.authenticated);
			break;

		case "CONTROLLER_UNREACHABLE_FROM_HTTPS":
			// Controller isn't accessible via https. Redirect to http and try again.
			window.location.href = window.location.href.replace("https", "http");
			break;

		case "HTTPS_CERT_MISSING":
			var params = {};
			var currentParams = this.__currentParams[0];
			if (currentParams.token) {
				params.token = currentParams.token;
			}
			this.history.navigate("installcert", {
				force: true,
				params: [params]
			});
			break;

		case "SERVICE_UNAVAILABLE":
			this.__handleServiceUnavailable(event.status);
			break;
		}
	},

	__handleConfigReady: function () {
		var started = this.__started || false;
		if ( !started ) {
			this.__started = true;
			this.ready();
		}
	},

	__handleAuthChange: function (authenticated) {
		this.__renderNavComponent();

		if (authenticated) {
			Config.client.openEventStream();
		} else if ( !this.__isLoginPath() ) {
			var currentHandler = this.history.getHandler();
			if (currentHandler && currentHandler.opts.auth === false) {
				// Don't redirect to login from page not requiring auth
				return;
			}

			this.waitForNav.then(function() {
				if ( !this.__isLoginPath() ) {
					this.__redirectToLogin();
				}
			}.bind(this));
		}
	},

	__handleGithubAuthChange: function (authenticated) {
		if (authenticated) {
			if ( !Config.githubClient ) {
				var githubAuth = Config.user.auths.github;
				Config.setGithubClient(new GithubClient(
					githubAuth.access_token,
					Config.github_api_url
				));
			}
		} else {
			Config.setGithubClient(null);
		}
	},

	__handleServiceUnavailable: function (status) {
		React.render(
			React.createElement(ServiceUnavailableComponent, { status: status }),
			document.getElementById('main')
		);
	},

	__handleHandlerBeforeEvent: function (event) {
		// prevent route handlers requiring auth from being called when app is not authenticated
		if ( !Config.authenticated && event.handler.opts.auth !== false ) {
			event.abort();
			if ( !this.__isLoginPath() ) {
				this.__redirectToLogin();
			}
			return;
		}

		Config.waitForRouteHandler = new Promise(function (rs) {
			this.__waitForRouteHandlerResolve = rs;
		}.bind(this));

		this.__renderNavComponent();

		if (event.handler.opts.secondary) {
			// view is rendered in a modal
			this.__secondary = true;
			return;
		}

		var path = event.path;

		// don't reset view if only params changed
		var prevPath = this.history.prevPath || "";
		if (path.split('?')[0] === prevPath.split('?')[0]) {
			if (event.handler.opts.paramChangeScrollReset !== false) {
				// reset scroll position
				window.scrollTo(0,0);
			}
			return;
		}

		// don't reset view when navigating between login/reset and login
		if (path.substr(0, 5) === "login" && prevPath.substr(0, 5) === "login") {
			return;
		}

		// unmount main view / reset scroll position
		if ( !event.handler.opts.secondary ) {
			window.scrollTo(0,0);

			// don't reset view when navigating in/out app modals or between provider views
			var pathRegexp = /^(?:apps\/[^\/]+)|providers/;
			if ((path.match(pathRegexp) || [1])[0] !== (prevPath.match(pathRegexp) || [2])[0]) {
				this.primaryView = null;
				React.unmountComponentAtNode(this.el);
			}
		}

		// unmount secondary view
		if (this.__secondary) {
			this.__secondary = false;
			React.unmountComponentAtNode(this.secondaryEl);
		}
	},

	__handleHandlerAfterEvent: function () {
		if (this.__waitForRouteHandlerResolve) {
			this.__waitForRouteHandlerResolve();
			Config.waitForRouteHandler = Promise.resolve();
		}
	}
});

export default Dashboard;
