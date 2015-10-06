import { extend } from 'marbles/utils';
import State from 'marbles/state';
import WaitGroup from 'dashboard/waitgroup';

var DEFAULT_N_PAGES = 3;

/*
 * @param props Object
 *   Store: Subclass of marbles/store responsible for fetching data
 *   storeID: ID object to use with Store
 *   fetchPrevPage: Function for fetching prev page
 *   fetchNextPage: Function for fetching next page
 */
var Paginator = function (props) {
	this.props = props;
	this.Store = props.Store;
	this.storeID = props.storeID;
	this.wgs = [new WaitGroup(), new WaitGroup()]; // prevPage, nextPage
	this.wgs[1].addOne();
	this.__changeListeners = [];
	this.handleStoreChange = this.handleStoreChange.bind(this);
	this.Store.addChangeListener(this.storeID, this.handleStoreChange);
	this.state = this.getInitialState();
};

extend(Paginator.prototype, State, {
	// Public

	close: function () {
		this.Store.removeChangeListener(this.storeID, this.handleStoreChange);
	},

	getState: function () {
		return {
			pages: this.state.pages,
			hasPrevPage: this.state.hasPrevPage,
			hasNextPage: this.state.hasNextPage
		};
	},

	unloadPage: function (pageID) {
		this.callAfterWaitGroup(this.__unloadPage.bind(this, pageID));
	},

	loadPrevPage: function (__wait) {
		if (__wait !== false) {
			return this.callAfterWaitGroup(this.__loadPrevPage.bind(this), 0);
		}
	},

	loadNextPage: function (__wait) {
		if (__wait !== false) {
			return this.callAfterWaitGroup(this.__loadNextPage.bind(this), 1);
		}
	},

	// Private

	getInitialState: function () {
		return {
			pages: [],
			hasPrevPage: true,
			hasNextPage: true,

			gHasPrevPage: true,
			gHasNextPage: true,
			allPages: [],
			unloadedPageIDsTop: [],
			unloadedPageIDsBottom: [],
			nEvents: 0
		};
	},

	__unloadPage: function (pageID) {
		var prevState = this.state;
		var nextState = {
			pages: [].concat(prevState.pages),
			unloadedPageIDsTop: [].concat(prevState.unloadedPageIDsTop),
			unloadedPageIDsBottom: [].concat(prevState.unloadedPageIDsBottom)
		};
		if (prevState.unloadedPageIDsTop.indexOf(pageID) !== -1 || prevState.unloadedPageIDsBottom.indexOf(pageID) !== -1) {
			window.console.error("Can't unload page that's already unloaded", pageID);
			return;
		}
		if (prevState.pages.length === 0) {
			window.console.error("Can't unload page as none are laoded");
			return;
		}
		if (prevState.pages[0].id === pageID) {
			nextState.unloadedPageIDsTop.push(pageID);
			nextState.pages = nextState.pages.slice(1);
			nextState.nEvents = prevState.nEvents - prevState.pages[0].events.length;
			nextState.hasPrevPage = true;
		} else if (prevState.pages[prevState.pages.length-1].id === pageID) {
			nextState.unloadedPageIDsBottom.unshift(pageID);
			nextState.pages = nextState.pages.slice(0, nextState.pages.length-1);
			nextState.nEvents = prevState.nEvents - prevState.pages[prevState.pages.length-1].events.length;
			nextState.hasNextPage = true;
		} else {
			window.console.error("Can't unload page that isn't first or last");
			return;
		}
		this.setState(nextState);
	},

	__loadPrevPage: function () {
		var prevState = this.state;
		if (prevState.unloadedPageIDsTop.length === 0) {
			this.fetchPrevPage();
			return;
		}
		var nextState = {};
		var pageIndex = prevState.unloadedPageIDsTop.length-1;
		nextState.unloadedPageIDsTop =prevState.unloadedPageIDsTop.slice(0, pageIndex);
		nextState.pages = prevState.allPages.slice(pageIndex, prevState.pages.length+pageIndex+1);
		nextState.nEvents = prevState.nEvents + nextState.pages[0].events.length;
		nextState.hasPrevPage = nextState.unloadedPageIDsTop.length > 0 || prevState.gHasPrevPage;
		this.setState(nextState);
	},

	__loadNextPage: function () {
		var prevState = this.state;
		if (prevState.unloadedPageIDsBottom.length === 0) {
			this.fetchNextPage();
			return;
		}
		var nextState = {};
		var offset = prevState.unloadedPageIDsTop.length;
		nextState.unloadedPageIDsBottom = prevState.unloadedPageIDsBottom.slice(1);
		nextState.pages = prevState.allPages.slice(offset, prevState.pages.length+1+offset);
		nextState.nEvents = prevState.nEvents + nextState.pages[nextState.pages.length-1].events.length;
		nextState.hasNextPage = nextState.unloadedPageIDsBottom.length > 0 || prevState.gHasNextPage;
		this.setState(nextState);
	},

	fetchPrevPage: function () {
		if (this.state.gHasPrevPage) {
			this.wgs[0].addOne();
			this.props.fetchPrevPage();
		} else {
			this.setState({
				hasPrevPage: false
			});
		}
	},

	fetchNextPage: function () {
		if (this.state.gHasNextPage) {
			this.wgs[1].addOne();
			this.props.fetchNextPage();
		} else {
			this.setState({
				hasNextPage: false
			});
		}
	},

	handleStoreChange: function () {
		var allowNewPageTop = !this.wgs[0].resolved;
		var allowNewPageBottom = !this.wgs[1].resolved;
		var prevState = this.state;
		var nextState = {};
		var gState = this.Store.getState(this.storeID);

		if (allowNewPageTop) {
			this.wgs[0].resolve();
		} else if (allowNewPageBottom) {
			this.wgs[1].resolve();
		}

		nextState.allPages = gState.pages;
		nextState.nEvents = prevState.nEvents;

		if (prevState.pages.length === 0) {
			nextState.pages = gState.pages.slice(0, DEFAULT_N_PAGES);
			nextState.nEvents = 0;
			nextState.pages.forEach(function (page) {
				nextState.nEvents += page.events.length;
			});
		} else if (gState.pages.length !== prevState.allPages.length) {
			// pages are only added, and only to either end (never both)
			var offset = gState.pages.length - prevState.allPages.length;
			if (allowNewPageBottom && prevState.allPages[0].id === gState.pages[0].id && prevState.unloadedPageIDsBottom.length === 0) {
				// load one new page bottom
				nextState.pages = gState.pages.slice(prevState.unloadedPageIDsTop.length, prevState.unloadedPageIDsTop.length + prevState.pages.length+1);
				nextState.nEvents = prevState.nEvents + nextState.pages[nextState.pages.length-1].events.length;
				nextState.unloadedPageIDsBottom = gState.pages.slice(prevState.unloadedPageIDsTop.length + prevState.pages.length + 1).map(function (page) {
					return page.id;
				});
			} else if (allowNewPageTop && prevState.allPages[prevState.allPages.length-1].id === gState.pages[gState.pages.length-1].id && prevState.unloadedPageIDsTop.length === 0) {
				// load one new page top
				nextState.pages = gState.pages.slice(offset-1, prevState.pages.length+1);
				nextState.nEvents = prevState.nEvents + nextState.pages[0].events.length;
				nextState.unloadedPageIDsTop = gState.pages.slice(0, offset-1).map(function (page) {
					return page.id;
				});
			}
		}

		var pages = nextState.pages || prevState.pages;
		if (prevState.unloadedPageIDsTop.length > 0 || (pages.length > 0 && pages[0].id !== nextState.allPages[0].id)) {
			nextState.hasPrevPage = true;
		} else {
			nextState.hasPrevPage = gState.hasPrevPage;
		}
		if (prevState.unloadedPageIDsBottom.lenght > 0 || (pages.length > 0 && pages[pages.length-1].id !== nextState.allPages[nextState.allPages.length-1].id)) {
			nextState.hasNextPage = true;
		} else {
			nextState.hasNextPage = gState.hasNextPage;
		}

		nextState.gHasPrevPage = gState.hasPrevPage;
		nextState.gHasNextPage = gState.hasNextPage;

		this.setState(nextState);

		var unloadedPageIDsTop = nextState.unloadedPageIDsTop || prevState.unloadedPageIDsTop;
		if (nextState.nEvents < 9 && nextState.hasPrevPage && unloadedPageIDsTop.length === 0) {
			// ensure new events are added when there are too few events to scroll
			this.fetchPrevPage();
		}
	},

	callAfterWaitGroup: function (fn, index) {
		if (index !== undefined && this.wgs[index].resolved) {
			if (index === 0 && !this.wgs[1].resolved) {
				this.wgs[1].resolve();
			} else if (index === 1 && !this.wgs[0].resolved) {
				this.wgs[0].resolve();
			}
			return fn();
		} else if (index !== undefined) {
			return this.wgs[index].then(this.callAfterWaitGroup.bind(this, fn, index));
		} else if (index === undefined && this.wgs[0].resolved && this.wgs[1].resolved) {
			return fn();
		} else {
			return Promise.all(this.wgs).then(this.callAfterWaitGroup.bind(this, fn, index));
		}
	}
});

export default Paginator;
