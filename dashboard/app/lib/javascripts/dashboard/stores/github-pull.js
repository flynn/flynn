//= require ../dispatcher
//= require ../store

(function () {
"use strict";

var GithubPull = Dashboard.Stores.GithubPull = Dashboard.Store.createClass({
	displayName: "Stores.GithubPull",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = this.id;
	},

	getInitialState: function () {
		return {
			pull: null,
			mergeJob: null
		};
	},

	didBecomeActive: function () {
		this.__fetchPull();
	},

	handleEvent: function (event) {
		switch (event.name) {
			case "APP_SOURCE_HISTORY:MERGE_PULL_REQUEST":
				(this.state.pull ? Promise.resolve() : this.__fetchPull()).then(this.__merge.bind(this));
			break;
		}
	},

	__fetchPull: function () {
		var pull = Dashboard.Stores.GithubPulls.findPull(this.props.ownerLogin, this.props.repoName, this.props.number);
		if (pull) {
			this.setState({
				pull: pull
			});
			return Promise.resolve(pull);
		}

		return Dashboard.githubClient.getPull(this.props.ownerLogin, this.props.repoName, this.props.number).then(function (args) {
			var res = args[0];
			var pull = this.__rewriteJSON(res);
			this.setState({
				pull: pull
			});
			return pull;
		}.bind(this));
	},

	__rewriteJSON: function (pullJSON) {
		var stripHTML = function (str) {
			var tmp = document.createElement("div");
			tmp.innerHTML = str;
			return tmp.textContent || tmp.innerText;
		};
		return {
			id: pullJSON.id,
			number: pullJSON.number,
			title: pullJSON.title,
			body: stripHTML(pullJSON.body),
			url: pullJSON.html_url,
			createdAt: pullJSON.created_at,
			updatedAt: pullJSON.updated_at,
			user: {
				login: pullJSON.user.login,
				avatarURL: pullJSON.user.avatar_url
			},
			head: {
				label: pullJSON.head.label,
				ref: pullJSON.head.ref,
				sha: pullJSON.head.sha,
				name: pullJSON.head.repo.name,
				ownerLogin: pullJSON.head.repo.owner.login,
				fullName: pullJSON.head.repo.full_name
			},
			base: {
				label: pullJSON.base.label,
				ref: pullJSON.base.ref,
				sha: pullJSON.base.sha,
				name: pullJSON.base.repo.name,
				ownerLogin: pullJSON.base.repo.owner.login,
				fullName: pullJSON.base.repo.full_name
			}
		};
	},

	__merge: function () {
		var pull = this.state.pull;
		var base = pull.base;
		var head = pull.head;

		var job = {
			type: "merge",
			base: base,
			head: head,
			status: "pending",
			mergeCommit: null
		};

		this.setState({
			mergeJob: job
		});

		var client = Dashboard.githubClient;

		client.mergePull(
			base.ownerLogin,
			base.name,
			pull.number,
			"Merge "+ head.fullName +" at "+ head.ref
		).then(function (args) {
			var res = args[0];

			job.status = "success";
			this.setState({
				mergeJob: job
			});

			Dashboard.Dispatcher.handleStoreEvent({
				name: "GITHUB_PULL:MERGED",
				pull: pull,
				mergeCommitSha: res.sha
			});

			return Dashboard.Stores.GithubCommit.getCommit({
				ownerLogin: base.ownerLogin,
				repoName: base.name,
				sha: res.sha
			}).catch(function(){});
		}.bind(this)).then(function (commit) {
			if ( !commit ) {
				return null;
			}

			job.mergeCommit = commit;
			this.setState({
				mergeJob: job
			});
		}.bind(this)).catch(function (args) {
			var res = args[0];
			var xhr = args[1];

			job.status = "failure";
			job.errorMsg = res.message || "Something went wrong ["+ xhr.status +"]";
			this.setState({
				mergeJob: job
			});

			Dashboard.Dispatcher.handleStoreEvent({
				name: "GITHUB_PULL:MERGE_FAILURE",
				pull: pull,
				errorMsg: job.errorMsg
			});
		}.bind(this));
	}
});

GithubPull.isValidId = function (id) {
	return id.ownerLogin && id.repoName && id.number;
};

GithubPull.registerWithDispatcher(Dashboard.Dispatcher);

})();
