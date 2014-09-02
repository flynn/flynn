//= require ../store

(function () {
"use strict";

var GithubRepo = FlynnDashboard.Stores.GithubRepo = FlynnDashboard.Store.createClass({
	displayName: "Stores.GithubRepo",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = this.id;
	},

	getInitialState: function () {
		return {
			repo: null
		};
	},

	didBecomeActive: function () {
		this.__fetchRepo();
	},

	__fetchRepo: function () {
		var repo = FlynnDashboard.Stores.GithubRepos.findRepo(this.props.ownerLogin, this.props.name);
		if (repo) {
			this.setState({
				repo: repo
			});
			return Promise.resolve(repo);
		}

		return FlynnDashboard.githubClient.getRepo(this.props.ownerLogin, this.props.name).then(function (args) {
			var res = args[0];
			var repo = this.__rewriteJSON(res);
			this.setState({
				repo: repo
			});
			return repo;
		}.bind(this));
	},

	__rewriteJSON: function (repoJSON) {
		return {
			id: repoJSON.id,
			name: repoJSON.name,
			language: repoJSON.language,
			description: repoJSON.description,
			ownerLogin: repoJSON.owner.login,
			defaultBranch: repoJSON.default_branch,
			cloneURL: repoJSON.clone_url
		};
	}
});

GithubRepo.isValidId = function (id) {
	return id.ownerLogin && id.name;
};

})();
