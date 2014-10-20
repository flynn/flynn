//= require ../store

(function () {
"use strict";

var GithubCommit = Dashboard.Stores.GithubCommit = Dashboard.Store.createClass({
	displayName: "Stores.GithubCommit",

	getState: function () {
		return this.state;
	},

	getCommit: function () {
		if (this.state.commit) {
			return Promise.resolve(this.state.commit);
		} else {
			return this.__fetchCommit();
		}
	},

	willInitialize: function () {
		this.props = this.id;
	},

	getInitialState: function () {
		return {
			commit: null
		};
	},

	didBecomeActive: function () {
		this.__fetchCommit();
	},

	__fetchCommit: function () {
		var commit = Dashboard.Stores.GithubCommits.findCommit(this.props.ownerLogin, this.props.repoName, this.props.sha);
		if (commit) {
			this.setState({
				commit: commit
			});
			return Promise.resolve(commit);
		}

		return Dashboard.githubClient.getCommit(this.props.ownerLogin, this.props.repoName, this.props.sha).then(function (args) {
			var res = args[0];
			var commit = this.__rewriteJSON(res);
			this.setState({
				commit: commit
			});
			return commit;
		}.bind(this));
	},

	__rewriteJSON: function (commitJSON) {
		var committer = commitJSON.committer || commitJSON.commit.committer;
		var author = commitJSON.author || commitJSON.commit.author;
		return {
			committer: {
				avatarURL: committer.avatar_url,
				name: commitJSON.commit.committer.name
			},
			author: {
				avatarURL: author.avatar_url,
				name: commitJSON.commit.author.name
			},
			committedAt: Date.parse(commitJSON.commit.committer.date),
			createdAt: Date.parse(commitJSON.commit.author.date),
			sha: commitJSON.sha,
			message: commitJSON.commit.message,
			githubURL: commitJSON.html_url
		};
	}
});

GithubCommit.isValidId = function (id) {
	return id.ownerLogin && id.repoName && id.sha;
};

})();
