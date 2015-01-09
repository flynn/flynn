//= require marbles/utils
//= require marbles/dispatcher
//= require marbles/store
//= require marbles/http
//= require marbles/http/middleware/serialize_json

(function () {
"use strict";

var ENGINE_KEY = "ybTG5YYdw6_Tf_BTdHC2";

var InputSelection = function (el) {
	this.el = el;
	this.start = this.calculateStart();
	this.end = this.calculateEnd();
};

Marbles.Utils.extend(InputSelection.prototype, {
	calculateStart: function () {
		var el = this.el;
		if (el.createTextRange) {
			var r = document.selection.createRange().duplicate();
			r.moveEnd('character', el.value.length);
			if (r.text === '') {
				return el.value.length;
			}
			return el.value.lastIndexOf(r.text);
		} else {
			return el.selectionStart;
		}
	},

	calculateEnd: function () {
		var el = this.el;
		if (el.createTextRange) {
			var r = document.selection.createRange().duplicate();
			r.moveStart('character', -el.value.length);
			return r.text.length;
		} else {
			return el.selectionEnd;
		}
	}
});

var SearchStore = Marbles.Store.createClass({
	getState: function () {
		return this.state;
	},

	setProps: function (props) {
		this.props = Marbles.Utils.extend({}, this.props, props);
		this.didReceiveProps(this.props);
	},

	getInitialState: function () {
		return {
			records: [],
			info: {
				query: ""
			},
			selectedIndex: -1,
			selectedRecord: null
		};
	},

	willInitialize: function () {
		this.props = {};
	},

	didBecomeActive: function () {
		this.__fetchResults();
	},

	didReceiveProps: function (props) {
		this.setState({
			info: {
				query: props.query
			}
		});
		this.__fetchResults();
	},

	didBecomeInactive: function () {
		if (this.__currentFetchRequest) {
			this.__currentFetchRequest.old = true;
		}
	},

	handleEvent: function (event) {
		switch (event.name) {
			case "SEARCH:SELECT_NEXT":
				this.__selectNext();
			break;

			case "SEARCH:SELECT_PREV":
				this.__selectPrev();
			break;
		}
	},

	__fetchResults: function () {
		if (this.__currentFetchRequest) {
			this.__currentFetchRequest.old = true;
		}

		var query = this.props.query || "";
		if (query === "") {
			this.setState(this.getInitialState());
			return;
		}

		var params = [{
			engine_key: this.props.engineKey,
			q: query
		}];

		var request = this.__currentFetchRequest = Marbles.HTTP({
			url: "https://api.swiftype.com/api/v1/public/engines/search.json",
			method: "GET",
			params: params,
			middleware: [Marbles.HTTP.Middleware.SerializeJSON]
		});
		request.then(function (args) {
			if (request.old) {
				return;
			}
			var res = args[0];
			var info = res.info.page;
			var records = res.records.page.map(this.__rewriteRecordJSON);
			this.setState({
				records: records,
				info: {
					query: info.query
				},
				selectedIndex:	-1,
				selectedRecord: null
			});
		}.bind(this));
	},

	__rewriteRecordJSON: function (recordJSON) {
		recordJSON.path = recordJSON.url.replace(/^https?:\/\/[^\/]+/, '');
		return recordJSON;
	},

	__selectNext: function () {
		var selectedIndex = this.state.selectedIndex;
		var minIndex = 0;
		var maxIndex = this.state.records.length-1;
		if (selectedIndex === maxIndex) {
			selectedIndex = minIndex;
		} else {
			selectedIndex += 1;
		}
		this.__selectIndex(selectedIndex);
	},

	__selectPrev: function () {
		var selectedIndex = this.state.selectedIndex;
		var minIndex = 0;
		var maxIndex = this.state.records.length-1;
		if (selectedIndex === minIndex) {
			selectedIndex = maxIndex;
		} else {
			selectedIndex -= 1;
		}
		this.__selectIndex(selectedIndex);
	},

	__selectIndex: function (index) {
		var selectedRecord = this.state.records[index] || null;
		if ( !selectedRecord ) {
			index = -1;
		}
		this.setState({
			selectedIndex: index,
			selectedRecord: selectedRecord
		});
	}
});

SearchStore.registerWithDispatcher(Marbles.Dispatcher);

var SearchBarComponent = React.createClass({
	displayName: "SearchBarComponent",

	focusInput: function () {
		this.refs.input.getDOMNode().focus();
	},

	render: function () {
		return React.createElement("input", {
			ref: "input",
			type: "text",
			placeholder: "Search",
			value: this.props.query,
			onChange: this.__handleQueryChange,
			onKeyDown: this.__handleKeyDown
		});
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		SearchStore.setProps(this.state.searchStoreId, this.__getSearchStoreProps(this.props));
		SearchStore.addChangeListener(this.state.searchStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var oldSearchStoreId = this.state.searchStoreId;
		var newSearchStoreId = this.__getSearchStoreId(props);
		if ( !Marbles.Utils.assertEqual(oldSearchStoreId, newSearchStoreId) ) {
			SearchStore.removeChangeListener(oldSearchStoreId, this.__handleStoreChange);
			SearchStore.addChangeListener(newSearchStoreId, this.__handleStoreChange);
			this.__handleStoreChange(props);
		}
		SearchStore.setProps(newSearchStoreId, this.__getSearchStoreProps(props));
	},

	componentWillUnmount: function () {
		SearchStore.removeChangeListener(this.state.searchStoreId, this.__handleStoreChange);
	},

	__getState: function (props) {
		var state = {
			searchStoreId: this.__getSearchStoreId(props)
		};

		var searchStoreState = SearchStore.getState(state.searchStoreId);
		state.records = searchStoreState.records;
		state.selectedRecord = searchStoreState.selectedRecord;

		return state;
	},

	__getSearchStoreId: function (props) {
		return props.engineKey;
	},

	__getSearchStoreProps: function (props) {
		return {
			query: (props.query || "").trim(),
			engineKey: props.engineKey
		};
	},

	__handleStoreChange: function (props) {
		props = props || this.props;
		this.setState(this.__getState(props));
	},

	__handleQueryChange: function (e) {
		var query = e.target.value;
		this.props.eventCallback({
			name: "QUERY_CHANGE",
			query: query
		});
	},

	__handleKeyDown: function (e) {
		if (this.state.records.length > 0) {
			this.props.eventCallback({
				name: "KEY_DOWN",
				key: e.key,
				ctrlKey: e.ctrlKey,
				metaKey: e.metaKey,
				charCode: e.charCode,
				keyCode: e.keyCode,
				preventDefault: e.preventDefault.bind(e),
				selectedRecord: this.state.selectedRecord
			});
		}
	}
});

var SearchResultsItemComponent = React.createClass({
	displayName: "SearchResultsItemComponent",

	render: function () {
		var record = this.props.record;
		return (
			<li className={this.props.selected ? "selected" : ""}>
				<a href={record.path}>{record.title}</a>
				<p>
					<div dangerouslySetInnerHTML={{ __html: (record.highlight.body || record.body.slice(0, 512) + "...").replace('§ ', '', 'g') }} />
				</p>
			</li>
		);
	},

	componentDidMount: function () {
		if (this.props.selected) {
			this.__scrollIntoView();
		}
	},

	componentDidUpdate: function () {
		if (this.props.selected) {
			this.__scrollIntoView();
		}
	},

	__scrollIntoView: function () {
		var el = this.getDOMNode();
		var offsetTop = el.offsetTop;
		var offsetHeight = el.offsetHeight;
		var parentsOffsetTop = 0;
		var offsetParent;
		var ref = el;
		while (offsetParent = ref.offsetParent) {
			parentsOffsetTop += offsetParent.offsetTop;
			ref = offsetParent;
		}
		var viewportHeight = window.innerHeight;
		if (((offsetTop + parentsOffsetTop + offsetHeight) > viewportHeight) || (offsetTop + parentsOffsetTop < window.scrollY)) {
			window.scrollTo(window.scrollX, offsetTop + offsetHeight + 20);
		}
	}
});

var SearchResultsComponent = React.createClass({
	displayName: "SearchResultsComponent",

	render: function () {
		if (this.state.query.length === 0 && this.props.originalHTML) {
			return React.createElement('div', { dangerouslySetInnerHTML: { __html: this.props.originalHTML } });
		}

		var selectedIndex = this.state.selectedIndex;

		return (
			<div className="search-results">
				<h1>Search Results</h1>
				{this.state.records.length > 0 ? (
					<ul>
						{this.state.records.map(function (record, index) {
							return (
								<SearchResultsItemComponent key={record.id} selected={index === selectedIndex} record={record} />
							);
						})}
					</ul>
				) : (
					<p>No results</p>
				)}
			</div>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		SearchStore.addChangeListener(this.state.searchStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var oldSearchStoreId = this.state.searchStoreId;
		var newSearchStoreId = this.__getSearchStoreId(props);
		if ( !Marbles.Utils.assertEqual(oldSearchStoreId, newSearchStoreId) ) {
			SearchStore.removeChangeListener(oldSearchStoreId, this.__handleStoreChange);
			SearchStore.addChangeListener(newSearchStoreId, this.__handleStoreChange);
			this.__handleStoreChange(props);
		}
	},

	componentDidUpdate: function () {
	},

	componentWillUnmount: function () {
		SearchStore.removeChangeListener(this.state.searchStoreId, this.__handleStoreChange);
	},

	__getState: function (props) {
		var state = {
			searchStoreId: this.__getSearchStoreId(props)
		};

		var searchStoreState = SearchStore.getState(state.searchStoreId);
		state.records = searchStoreState.records;
		state.query = searchStoreState.info.query;
		state.selectedIndex = searchStoreState.selectedIndex;
		state.selectedRecord = searchStoreState.selectedRecord;

		return state;
	},

	__getSearchStoreId: function (props) {
		return props.engineKey;
	},

	__handleStoreChange: function (props) {
		props = props || this.props;
		this.setState(this.__getState(props));
	}
});

var searchResultsEl = document.getElementById("main");
var originalHTML = window.location.pathname.match(/^\/search/) ? null : searchResultsEl.innerHTML;
React.render(React.createElement(SearchResultsComponent, {
	engineKey: ENGINE_KEY,
	originalHTML: originalHTML
}), searchResultsEl);

var SearchBarRenderer = function (el) {
	this.el = el;
	this.props = {};
	this.props.engineKey = ENGINE_KEY;
	this.props.eventCallback = this.handleViewEvent.bind(this);
};

SearchBarRenderer.prototype.render = function (props) {
	props = Marbles.Utils.extend({}, this.props, props);
	if ( !this.component ) {
		this.component = React.render(React.createElement(SearchBarComponent, props), this.el);
	} else {
		this.component.setProps(props);
	}
};

SearchBarRenderer.prototype.focusInput = function () {
	if (this.component) {
		this.component.focusInput();
	}
};

SearchBarRenderer.prototype.handleViewEvent = function (event) {
	switch (event.name) {
		case "QUERY_CHANGE":
			this.render({
				query: event.query
			});
		break;

		case "KEY_DOWN":
			if (event.key === "ArrowDown" || (event.key === "j" && event.ctrlKey)) {
				event.preventDefault();
				Marbles.Dispatcher.dispatch({
					name: "SEARCH:SELECT_NEXT"
				});
			} else if (event.key === "ArrowUp" || (event.key === "k" && event.ctrlKey)) {
				var inputEl = this.component.refs.input.getDOMNode();
				var selection = new InputSelection(this.component.refs.input.getDOMNode());
				if (selection.start === inputEl.value.length) {
					event.preventDefault();
					Marbles.Dispatcher.dispatch({
						name: "SEARCH:SELECT_PREV"
					});
				}
			} else if (event.key === "Enter" && event.selectedRecord) {
				event.preventDefault();
				var url = window.location.href.replace(/^(https?:\/\/[^\/]+).*$/, '$1') + event.selectedRecord.path;
				if (event.ctrlKey || event.metaKey) {
					window.open(url);
				} else {
					window.location.href = url;
				}
			}
		break;
	}
};

var navSidebarEl = document.querySelector('.nav-list.sidebar');
SearchStore.addChangeListener(ENGINE_KEY, function () {
	var query = SearchStore.getState(ENGINE_KEY).info.query;
	if (query.length === 0) {
		navSidebarEl.classList.remove("no-active");
	} else {
		navSidebarEl.classList.add("no-active");
	}
});

var searchBars = Array.prototype.map.call(document.querySelectorAll(".search-bar"), function (el) {
	var searchBarRenderer = new SearchBarRenderer(el);
	searchBarRenderer.render();
	return searchBarRenderer;
});

var originalPath = window.location.pathname;
SearchStore.addChangeListener(ENGINE_KEY, function () {
	var query = SearchStore.getState(ENGINE_KEY).info.query;
	if (query.length === 0) {
		if ( !originalPath.match(/^\/search/) && window.location.pathname.match(/^\/search/) ) {
			window.history.pushState({}, document.title, originalPath);
		} else if (window.location.pathname.match(/^\/search/)) {
			window.history.replaceState({}, document.title, "/search");
		}
	} else {
		var method = "pushState";
		if (window.location.pathname.match(/^\/search/)) {
			method = "replaceState";
		}
		window.history[method]({}, document.title, "/search?q="+ encodeURIComponent(query));
	}
});

function initSearch() {
	var queryString = window.location.search.replace(/^\?/, '');
	var queryParams = {};
	queryString.split("&").forEach(function (item) {
		var parts = item.split("=");
		var key = decodeURIComponent(parts[0]);
		var val = decodeURIComponent(parts[1] || "");
		queryParams[key] = val;
	});
	searchBars[0].handleViewEvent({
		name: "QUERY_CHANGE",
		query: queryParams.q
	});
}
if (originalPath.match(/^\/search/)) {
	initSearch();
}

window.addEventListener("keydown", function (e) {
	if (e.keyCode === 191) { // "/"
		e.preventDefault();
		searchBars[0].focusInput();
	}
}, false);

var ignorePopstate = true;
window.addEventListener('load', function () {
	setTimeout(function () {
		ignorePopstate = false;
	}, 200);
});
window.addEventListener("popstate", function () {
	if (window.location.pathname.match(/^\/search/)) {
		initSearch();
	} else if ( !ignorePopstate ) {
		window.location.reload();
	}
});

})();
