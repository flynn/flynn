import QueryParams from 'marbles/query_params';
import LinkHeader from 'marbles/http/link_header';
import Store from '../store';
import Config from '../config';

var GithubBranches = Store.createClass({
	displayName: "Stores.GithubBranches",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = this.id;
	},

	getInitialState: function () {
		return {
			branchNames: []
		};
	},

	didBecomeActive: function () {
		this.__fetchBranches();
	},

	__fetchBranches: function (options) {
		options = options || {};
		var params = options.params || [{}];

		Config.githubClient.getBranches(this.props.ownerLogin, this.props.repoName, params).then(function (args) {
			var res = args[0];
			var xhr = args[1];

			var parseLinkParams = function (rel, links) {
				var link = null;
				for (var i = 0, len = links.length; i < len; i++) {
					if (links[i].rel === rel) {
						link = links[i];
						break;
					}
				}
				if (link === null) {
					return null;
				}
				return QueryParams.deserializeParams(link.href.split("?")[1]);
			};

			var links = LinkHeader.parse(xhr.getResponseHeader("Link") || "");
			var nextParams = parseLinkParams("next", links);

			this.setState({
				branchNames: this.state.branchNames.concat(res.map(this.__rewriteJSON))
			});

			if (nextParams) {
				this.__fetchBranches({ params: nextParams });
			}
		}.bind(this));
	},

	__rewriteJSON: function (branchJSON) {
		return branchJSON.name;
	}
});

GithubBranches.isValidId = function (id) {
	return id.ownerLogin && id.repoName;
};

export default GithubBranches;
