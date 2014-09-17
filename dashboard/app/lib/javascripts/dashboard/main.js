(function () {

"use strict";

window.Dashboard = {
	Stores: {},
	Views: {
		Models: {},
		Helpers: {}
	},
	Actions: {},
	routers: {},
	config: {},

	waitForRouteHandler: Promise.resolve(),

	run: function () {
		if ( Marbles.history && Marbles.history.started ) {
			throw new Error("Marbles.history already started!");
		}

		this.client = new this.Client(this.config.endpoints);

		if (this.config.user && this.config.user.auths.github) {
			Dashboard.githubClient = new Dashboard.GithubClient(
				this.config.user.auths.github.access_token
			);
		}

		this.el = document.getElementById("main");
		this.secondaryEl = document.getElementById("secondary");

		this.__secondary = false;

		Marbles.History.start({
			root: (this.config.PATH_PREFIX || '') + '/',
			dispatcher: this.Dispatcher
		});
	},

	__isLoginPath: function () {
		var path = Marbles.history.path;
		if ( path === "" ) {
			return false;
		}
		return String(path).substr(0, 5) === 'login';
	},

	__redirectToLogin: function () {
		var redirectPath = Marbles.history.path ? '?redirect='+ encodeURIComponent(Marbles.history.path) : '';
		Marbles.history.navigate('login'+ redirectPath);
	},

	__handleEvent: function (event) {
		if (event.source === "Marbles.History") {
			switch (event.name) {
				case "handler:before":
					this.waitForRouteHandler = new Promise(function (rs) {
						this.__waitForRouteHandlerResolve = rs;
					}.bind(this));

					// prevent route handlers requiring auth from being called when app is not authenticated
					if ( !this.config.authenticated && event.handler.opts.auth !== false ) {
						event.abort();
						return;
					}

					if (event.handler.opts.secondary) {
						// view is rendered in a modal
						this.__secondary = true;
						return;
					}

					var path = event.path;

					// don't reset view if only params changed
					var prevPath = Marbles.history.prevPath || "";
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
						this.primaryView = null;
						React.unmountComponentAtNode(this.el);
					}

					// unmount secondary view
					if (this.__secondary) {
						this.__secondary = false;
						React.unmountComponentAtNode(this.secondaryEl);
					}
				break;

				case "handler:after":
					if (this.__waitForRouteHandlerResolve) {
						this.__waitForRouteHandlerResolve();
						this.waitForRouteHandler = Promise.resolve();
					}
				break;
			}
			return;
		}

		if (event.name === "LOGOUT_BTN_CLICK") {
			Dashboard.client.logout();
		}

		if (event.source !== "APP_EVENT") {
			return;
		}
		var started = this.__started || false;
		switch (event.name) {
			case "CONFIG_READY":
				if ( !started ) {
					this.__started = true;
					this.run();
				}
			break;

			case "AUTH_CHANGE":
				this.__handleAuthChange(event.authenticated);
			break;

			case "GITHUB_AUTH_CHANGE":
				this.__handleGithubAuthChange(event.authenticated);
			break;

			case "SERVICE_UNAVAILABLE":
				this.__handleServiceUnavailable(event.status);
			break;
		}
	},

	__handleAuthChange: function (authenticated) {
		if ( !authenticated && !this.__isLoginPath() ) {
			this.__redirectToLogin();
		}
	},

	__handleGithubAuthChange: function (authenticated) {
		if (authenticated) {
			if ( !this.githubClient ) {
				var githubAuth = this.config.user.auths.github;
				this.githubClient = new this.GithubClient(
					githubAuth.access_token
				);
			}
		} else {
			this.githubClient = null;
		}
	},

	__handleServiceUnavailable: function (status) {
		React.renderComponent(
			Dashboard.Views.ServiceUnavailable({ status: status }),
			document.getElementById('main')
		);
	}
};

})();
