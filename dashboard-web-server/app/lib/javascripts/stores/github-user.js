//= require ../store

(function () {
"use strict";

FlynnDashboard.Stores.GithubUser = FlynnDashboard.Store.createClass({
	displayName: "Stores.GithubUser",

	getState: function () {
		return this.state;
	},

	getInitialState: function () {
		return {
			user: null
		};
	},

	didBecomeActive: function () {
		this.__fetchUser();
	},

	__fetchUser: function () {
		FlynnDashboard.githubClient.getUser().then(function (args) {
			var res = args[0];
			this.setState({
				user: this.__rewriteJSON(res)
			});
		}.bind(this));
	},

	__rewriteJSON: function (userJSON) {
		return {
			avatarURL: userJSON.avatar_url,
			login: userJSON.login,
			name: userJSON.name
		};
	}

});

})();
