import Store from '../store';
import Config from '../config';
import GithubRepos from './github-repos';
import { rewriteGithubRepoJSON } from './github-repo-json';

var GithubRepo = Store.createClass({
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
		var repo = GithubRepos.findRepo(this.props.ownerLogin, this.props.name);
		if (repo) {
			this.setState({
				repo: repo
			});
			return Promise.resolve(repo);
		}

		return Config.githubClient.getRepo(this.props.ownerLogin, this.props.name).then(function (args) {
			var res = args[0];
			var repo = this.__rewriteJSON(res);
			this.setState({
				repo: repo
			});
			return repo;
		}.bind(this));
	},

	__rewriteJSON: function (repoJSON) {
		return rewriteGithubRepoJSON(repoJSON);
	}
});

GithubRepo.isValidId = function (id) {
	return id.ownerLogin && id.name;
};

export default GithubRepo;
