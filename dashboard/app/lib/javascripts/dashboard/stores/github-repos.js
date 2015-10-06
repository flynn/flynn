import QueryParams from 'marbles/query_params';
import LinkHeader from 'marbles/http/link_header';
import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';
import { rewriteGithubRepoJSON } from './github-repo-json';

var GithubRepos = Store.createClass({
	displayName: "Stores.GithubRepos",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = this.id;
		this.__cachedPages = {};
	},

	getInitialState: function () {
		return {
			pages: []
		};
	},

	didBecomeActive: function () {
		this.__fetchRepos({ operation: "append" });
	},

	handleEvent: function (event) {
		switch (event.name) {
		case "GITHUB_REPOS:UNLAOD_PAGE_ID":
			this.__unloadPageId(event.pageId);
			break;

		case "GITHUB_REPOS:FETCH_PREV_PAGE":
			this.__fetchPrevPage();
			break;

		case "GITHUB_REPOS:FETCH_NEXT_PAGE":
			this.__fetchNextPage();
			break;
		}
	},

	__unloadPageId: function (pageId) {
		var pages = this.state.pages;
		var pageIndex = -1;
		for (var i = 0, len = pages.length; i < len; i++ && len--) {
			if (pages[i].id === pageId) {
				pageIndex = i;
				break;
			}
			if (pages[len-1].id === pageId) {
				pageIndex = len-1;
				break;
			}
		}
		if (pageIndex !== -1) {
			pages = pages.slice(0, pageIndex).concat(pages.slice(pageIndex+1, pages.length));
			this.setState({
				pages: pages,
				prevPageParams: pages[0].prevParams,
				nextParams: pages[pages.length-1].nextParams
			});
		}
	},

	__fetchPrevPage: function () {
		if ( !this.state.prevPageParams ) {
			throw new Error(this.constructor.displayName + ": Invalid attempt to fetch prev page!");
		}
		this.__fetchRepos({
			params: this.state.prevPageParams,
			operation: "prepend"
		});
	},

	__fetchNextPage: function () {
		if ( !this.state.nextPageParams ) {
			throw new Error(this.constructor.displayName + ": Invalid attempt to fetch next page!");
		}
		this.__fetchRepos({
			params: this.state.nextPageParams,
			operation: "append"
		});
	},

	__fetchRepos: function (options) {
		var params = QueryParams.replaceParams.apply(null, [[{
			sort: "pushed",
			type: "owner"
		}]].concat(options.params || []));
		var getReposFn = Config.githubClient.getRepos;

		if (this.props.org) {
			getReposFn = Config.githubClient.getOrgRepos;
			params[0].org = this.props.org;

			if (this.props.type === "fork") {
				params[0].type = "forks";
			} else {
				params[0].type = "all";
			}
		} else if (this.props.type === "star") {
			getReposFn = Config.githubClient.getStarredRepos;
		}

		var pageId = String(params[0].page);
		if (pageId && this.__cachedPages[pageId]) {
			this.__addPage(this.__cachedPages[pageId], options.operation);
			return;
		}

		getReposFn.call(Config.githubClient, params).then(function (args) {
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
			var prevParams = parseLinkParams("prev", links);
			var nextParams = parseLinkParams("next", links);

			var pageId;
			if (prevParams) {
				pageId = Number(prevParams[0].page) + 1;
			} else if (nextParams) {
				pageId = Number(nextParams[0].page) - 1;
			} else {
				pageId = 1;
			}

			if (this.props.type === "fork") {
				res = res.filter(function (repoJSON) {
					return repoJSON.fork;
				}, this);
			} else if (this.props.type !== "star") {
				res = res.filter(function (repoJSON) {
					return !repoJSON.fork;
				}, this);
			}

			var page = {
				id: pageId,
				prevParams: prevParams,
				nextParams: nextParams,
				repos: res.map(this.__rewriteJSON)
			};
			this.__cachedPages[String(page.id)] = page;
			this.__addPage(page, options.operation);
		}.bind(this));
	},

	__addPage: function (page, operation) {
		var pages = this.state.pages;
		if (operation === "prepend") {
			pages = [page].concat(pages);
		} else if (operation === "append") {
			pages = pages.concat([page]);
		} else {
			throw new Error(this.constructor.displayName +": Invalid page operation: "+ JSON.stringify(operation));
		}

		var nextParams = pages[pages.length-1].nextParams;
		var prevParams = pages[0].prevParams;

		this.setState({
			pages: pages,
			prevPageParams: prevParams,
			nextPageParams: nextParams
		});
	},

	__rewriteJSON: function (repoJSON) {
		return rewriteGithubRepoJSON(repoJSON);
	}

});

GithubRepos.findRepo = function (owner, repo) {
	var instances = this.__instances;
	var instance;
	var pi; // page index
	var pages;
	var len;
	var i;
	var repos;
	var rlen;
	for (var k in instances) {
		if (instances.hasOwnProperty(k)) {
			instance = instances[k];
			if ( !instance.id.org || instance.id.org === owner ) {
				for (pi = 0, pages = instance.state.pages, len = pages.length; pi < len; pi++) {
					for (i = 0, repos = pages[pi].repos, rlen = repos.length; i < rlen; i++) {
						if (repos[i].ownerLogin === owner && repos[i].name === repo) {
							return repos[i];
						}
					}
				}
			}
		}
	}
	return null;
};

GithubRepos.dispatcherIndex = GithubRepos.registerWithDispatcher(Dispatcher);

export default GithubRepos;
