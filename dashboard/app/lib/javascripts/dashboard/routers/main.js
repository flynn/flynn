//= require ../views/apps
//= require ../views/login
//= require ../views/models/login
//= require ../views/install-cert

(function () {

"use strict";

var LoginModel = Dashboard.Views.Models.Login;

Dashboard.routers.main = new (Marbles.Router.createClass({
	displayName: "routers.main",

	routes: [
		{ path: "", handler: "root" },
		{ path: "login", handler: "login", auth: false },
		{ path: "installcert", handler: "installCert", auth: false },
	],

	root: function (params) {
		delete params[0].token;
		Marbles.history.navigate("/apps", {
			replace: true,
			params: params
		});
	},

	login: function (params) {
		var redirectPath = params[0].redirect || null;
		if (redirectPath && redirectPath.indexOf("//") !== -1) {
			redirectPath = null;
		}
		if ( !redirectPath ) {
			redirectPath = "";
		}

		var performRedirect = function () {
			Marbles.history.navigate(decodeURIComponent(redirectPath));
		};

		if (Dashboard.config.authenticated) {
			performRedirect();
			return;
		}

		if (params[0].token) {
			LoginModel.setValue("token", params[0].token);
			LoginModel.performLogin().then(function () {
				performRedirect();
			}).catch(function () {
				var paramsWithoutToken = [Marbles.Utils.extend({}, params[0], { token: null })];
				this.login(paramsWithoutToken);
			}.bind(this));
		} else {
			React.render(React.createElement(
				Dashboard.Views.Login, {
						onSuccess: performRedirect
					}), Dashboard.el);
		}
	},

	installCert: function (params) {
		if (window.location.protocol === "https:") {
			Marbles.history.navigate("", {params: params});
			return;
		}
		var handleSubmit = function (e) {
			e.preventDefault();
			Dashboard.__isCertInstalled().then(function () {
				Marbles.history.navigate("/login", {params: params});
			});
		};
		React.render(React.createElement("form", { onSubmit: handleSubmit},
			React.createElement("section", { className: "panel" },
				React.createElement(
					Dashboard.Views.InstallCert, {
						certURL: Dashboard.config.API_SERVER.replace("https", "http") + "/cert"
					}))), Dashboard.el);
	}

}))();

})();
