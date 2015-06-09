import Store from '../store';
import Config from '../config';
import { rewriteGithubCommitJSON } from './github-commit-json';
import GithubCommits from './github-commits';

var GithubCommit = Store.createClass({
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
		var commit = GithubCommits.findCommit(this.props.ownerLogin, this.props.repoName, this.props.sha);
		if (commit) {
			this.setState({
				commit: commit
			});
			return Promise.resolve(commit);
		}

		return Config.githubClient.getCommit(this.props.ownerLogin, this.props.repoName, this.props.sha).then(function (args) {
			var res = args[0];
			var commit = this.__rewriteJSON(res);
			this.setState({
				commit: commit
			});
			return commit;
		}.bind(this));
	},

	__rewriteJSON: function (commitJSON) {
		return rewriteGithubCommitJSON(commitJSON);
	}
});

GithubCommit.isValidId = function (id) {
	return id.ownerLogin && id.repoName && id.sha;
};

export default GithubCommit;
