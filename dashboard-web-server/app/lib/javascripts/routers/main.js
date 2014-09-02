//= require ../views/main
//= require ../views/login

(function () {

"use strict";

FlynnDashboard.routers.main = new (Marbles.Router.createClass({
	displayName: "routers.main",

	routes: [
		{ path: "", handler: "root" },
		{ path: "login", handler: "login", auth: false },
	],

	root: function () {
		React.renderComponent(
			FlynnDashboard.Views.Main({
					githubAuthed: !!FlynnDashboard.githubClient
				}), FlynnDashboard.el);
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
			React.unmountComponentAtNode(FlynnDashboard.config.containerEl);
			Marbles.history.navigate(decodeURIComponent(redirectPath));
		};

		if (FlynnDashboard.config.authenticated) {
			performRedirect();
			return;
		}

		React.renderComponent(
			FlynnDashboard.Views.Login({
					onSuccess: performRedirect
				}), FlynnDashboard.el);
	}

}))();

})();
