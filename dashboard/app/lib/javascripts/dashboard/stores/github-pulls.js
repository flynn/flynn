import QueryParams from 'marbles/query_params';
import LinkHeader from 'marbles/http/link_header';
import Store from '../store';
import Config from '../config';
import Dispatcher from '../dispatcher';
import { rewriteGithubPullJSON } from './github-pull-json';

var GithubPulls = Store.createClass({
	displayName: "Stores.GithubPulls",

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
		this.__fetchPulls({ operation: "append" });
	},

	handleEvent: function (event) {
		switch (event.name) {
		case "GITHUB_PULLS:UNLAOD_PAGE_ID":
			this.__unloadPageId(event.pageId);
			break;

		case "GITHUB_PULLS:FETCH_PREV_PAGE":
			this.__fetchPrevPage();
			break;

		case "GITHUB_PULLS:FETCH_NEXT_PAGE":
			this.__fetchNextPage();
			break;

		case "GITHUB_PULL:MERGED":
			this.__handlePullMerged(event.pull);
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
		this.__fetchPulls({
			params: this.state.prevPageParams,
			operation: "prepend"
		});
	},

	__fetchNextPage: function () {
		if ( !this.state.nextPageParams ) {
			throw new Error(this.constructor.displayName + ": Invalid attempt to fetch next page!");
		}
		this.__fetchPulls({
			params: this.state.nextPageParams,
			operation: "append"
		});
	},

	__fetchPulls: function (options) {
		var params = QueryParams.replaceParams.apply(null, [[{
			sort: "updated",
			direction: "desc"
		}]].concat(options.params || [{}]));

		Config.githubClient.getPulls(this.props.ownerLogin, this.props.repoName, params).then(function (args) {
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

			var pulls = res.filter(function (pullJSON) {
				return !!pullJSON.head.repo && !!pullJSON.base.repo;
			}).map(this.__rewriteJSON);

			if (pulls.length === 0) {
				if (this.state.pages.length === 0) {
					this.setState({
						empty: true
					});
				}

				// don't add an empty page
				return;
			}

			var page = {
				id: pageId,
				prevParams: prevParams,
				nextParams: nextParams,
				pulls: pulls
			};

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
			empty: false,
			pages: pages,
			prevPageParams: prevParams,
			nextPageParams: nextParams
		});
	},

	__handlePullMerged: function (pull) {
		var pages = this.state.pages;
		var pulls, j, jlen;
		var pullIndex = -1;
		var pullPageIndex = -1;
		for (var i = 0, len = pages.length; i < len; i++) {
			pulls = pages[i].pulls;
			for (j = 0, jlen = pulls.length; j < jlen; j++) {
				if (pulls[j].id === pull.id) {
					pullIndex = j;
					break;
				}
			}
			if (pullIndex > -1) {
				pullPageIndex = i;
				break;
			}
		}

		var page;
		if (pullPageIndex > -1 && pullIndex > -1) {
			page = pages[pullPageIndex];
			pulls = page.pulls;
			pulls = pulls.slice(0, pullIndex).concat(pulls.slice(pullIndex + 1));
			page.pulls = pulls;
		}

		this.setState({
			pages: pages
		});
	},

	__rewriteJSON: function (pullJSON) {
		return rewriteGithubPullJSON(pullJSON);
	}

});

GithubPulls.findPull = function (owner, repo, number) {
	var instances = this.__instances;
	var instance;
	var pi; // page index
	var pages;
	var len;
	var i;
	var pulls;
	var plen;
	for (var k in instances) {
		if (instances.hasOwnProperty(k)) {
			instance = instances[k];
			if (instance.id.ownerLogin === owner && instance.id.repoName === repo) {
				for (pi = 0, pages = instance.state.pages, len = pages.length; pi < len; pi++) {
					for (i = 0, pulls = pages[pi].pulls, plen = pulls.length; i < plen; i++) {
						if (pulls[i].number === number) {
							return pulls[i];
						}
					}
				}
			}
		}
	}
	return null;
};

GithubPulls.isValidId = function (id) {
	return id.ownerLogin && id.name;
};

GithubPulls.registerWithDispatcher(Dispatcher);

export default GithubPulls;
