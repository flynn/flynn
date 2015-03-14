//= require ../views/apps
//= require ../views/login
//= require ../views/install-cert

(function () {

"use strict";

Dashboard.routers.main = new (Marbles.Router.createClass({
	displayName: "routers.main",

	routes: [
		{ path: "", handler: "root" },
		{ path: "login", handler: "login", auth: false },
		{ path: "installcert", handler: "installCert", auth: false },
	],

	root: function (params) {
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
			React.unmountComponentAtNode(Dashboard.config.containerEl);
			Marbles.history.navigate(decodeURIComponent(redirectPath));
		};

		if (Dashboard.config.authenticated) {
			performRedirect();
			return;
		}

		React.render(React.createElement(
			Dashboard.Views.Login, {
					onSuccess: performRedirect
				}), Dashboard.el);
	},

	installCert: function () {
		if (window.location.protocol === "https:") {
			Marbles.history.navigate("");
			return;
		}
		React.render(React.createElement("form", { onSubmit: function () { Marbles.history.navigate("/"); } },
			React.createElement("section", { className: "panel" },
				React.createElement(
					Dashboard.Views.InstallCert, {
						certURL: Dashboard.config.API_SERVER.replace("https", "http") + "/cert"
					}))), Dashboard.el);
	}

}))();

})();
