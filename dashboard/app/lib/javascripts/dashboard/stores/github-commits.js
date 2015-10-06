import QueryParams from 'marbles/query_params';
import LinkHeader from 'marbles/http/link_header';
import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';
import { rewriteGithubCommitJSON } from './github-commit-json';

var GithubCommits = Store.createClass({
	displayName: "Stores.GithubCommits",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = this.id;
	},

	getInitialState: function () {
		return {
			empty: false,
			pages: []
		};
	},

	didBecomeActive: function () {
		var fetchOpts = {
			operation: "append"
		};
		if (this.props.refSha) {
			fetchOpts.refSha = this.props.refSha;
		}
		this.__unloadPageLocked = true;
		this.__fetchCommits(fetchOpts).then(function () {
			setTimeout(function () {
				this.__unloadPageLocked = false;
			}.bind(this), 300);
		}.bind(this));
	},

	handleEvent: function (event) {
		switch (event.name) {
		case "GITHUB_COMMITS:UNLAOD_PAGE_ID":
			this.__unloadPageId(event.pageId);
			break;

		case "GITHUB_COMMITS:FETCH_PREV_PAGE":
			this.__fetchPrevPage();
			break;

		case "GITHUB_COMMITS:FETCH_NEXT_PAGE":
			this.__fetchNextPage();
			break;
		}
	},

	__unloadPageId: function (pageId) {
		if (this.__unloadPageLocked) {
			return;
		}
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
		this.__fetchCommits({
			params: this.state.prevPageParams,
			operation: "prepend"
		});
	},

	__fetchNextPage: function () {
		if ( !this.state.nextPageParams ) {
			throw new Error(this.constructor.displayName + ": Invalid attempt to fetch next page!");
		}
		this.__fetchCommits({
			params: this.state.nextPageParams,
			operation: "append"
		});
	},

	__fetchCommits: function (options) {
		var params = QueryParams.replaceParams.apply(null, [[{
			sha: this.props.branch
		}]].concat(options.params || [{}]));

		var refSha = options.refSha || null;

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

		var client = Config.githubClient;
		var pages = [];
		var fetchCommits = function (ownerLogin, repoName, params) {
			return client.getCommits(ownerLogin, repoName, params).then(function (args) {
				var res = args[0];
				var xhr = args[1];

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

				var commits = res.map(this.__rewriteJSON);

				if (commits.length === 0) {
					if (this.state.pages.length === 0) {
						this.setState({
							empty: true
						});
					}

					// don't add an empty page
					return pages;
				}

				var hasRefSha = false;

				if (refSha) {
					for (var i = 0, len = commits.length; i < len; i++) {
						if (commits[i].sha === refSha) {
							hasRefSha = true;
							break;
						}
					}
				}

				pages.push({
					id: pageId,
					prevParams: prevParams,
					nextParams: nextParams,
					commits: commits,
					hasRefSha: hasRefSha
				});

				if (refSha && !hasRefSha && nextParams) {
					return fetchCommits(nextParams);
				} else {
					return pages;
				}
			}.bind(this));
		}.bind(this, this.props.ownerLogin, this.props.repoName);

		return fetchCommits(params).then(function (pages) {
			this.__addPages(pages, options.operation);
		}.bind(this));
	},

	__addPages: function (newPages, operation) {
		var pages = this.state.pages;
		if (operation === "prepend") {
			pages = newPages.concat(pages);
		} else if (operation === "append") {
			pages = pages.concat(newPages);
		} else {
			throw new Error(this.constructor.displayName +": Invalid page operation: "+ JSON.stringify(operation));
		}

		var nextParams = pages[pages.length-1].nextParams;
		var prevParams = pages[0].prevParams;

		this.setState({
			empty: false,
			pages: pages,
			prevPageParams: prevParams,
			nextPageParams: nextParams
		});
	},

	__rewriteJSON: function (commitJSON) {
		return rewriteGithubCommitJSON(commitJSON);
	}

});

GithubCommits.findCommit = function (owner, repo, sha) {
	var instances = this.__instances;
	var instance;
	var pi; // page index
	var pages;
	var len;
	var i;
	var commits;
	var clen;
	for (var k in instances) {
		if (instances.hasOwnProperty(k)) {
			instance = instances[k];
			if (instance.id.ownerLogin === owner && instance.id.repoName === repo) {
				for (pi = 0, pages = instance.state.pages, len = pages.length; pi < len; pi++) {
					for (i = 0, commits = pages[pi].commits, clen = commits.length; i < clen; i++) {
						if (commits[i].sha === sha) {
							return commits[i];
						}
					}
				}
			}
		}
	}
	return null;
};

GithubCommits.isValidId = function (id) {
	return id.ownerLogin && id.repoName && id.branch;
};

GithubCommits.registerWithDispatcher(Dispatcher);

export default GithubCommits;
