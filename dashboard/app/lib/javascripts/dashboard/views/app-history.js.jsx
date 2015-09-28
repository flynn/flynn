import { extend } from 'marbles/utils';
import State from 'marbles/state';
import AppHistoryStore from 'dashboard/stores/app-history';
import Dispatcher from 'dashboard/dispatcher';
import ScrollPagination from 'ScrollPagination';
import Timestamp from './timestamp';
import WaitGroup from 'dashboard/waitgroup';

var DEFAULT_N_PAGES = 3;
var AppHistoryPaginator = function (appID) {
	this.storeID = {appID: appID};
	this.allowNewPageTop = false;
	this.allowNewPageBottom = false;
	this.__changeListeners = [];
	this.__handleStoreChange = this.__handleStoreChange.bind(this);
	this.wg = new WaitGroup();
	AppHistoryStore.addChangeListener(this.storeID, this.__handleStoreChange);

	this.state = {
		nEvents: 0,
		allPages: [],
		pages: [],
		unloadedPageIDsTop: [],
		unloadedPageIDsBottom: [],
		hasNextPage: true,
		hasPrevPage: true
	};
};

extend(AppHistoryPaginator.prototype, State, {
	close: function () {
		AppHistoryStore.removeChangeListener(this.storeID, this.__handleStoreChange);
	},

	// Remove page with given ID from either end of state.pages
	unloadPage: function (pageID, wait) {
		if (wait !== false) {
			return this.wg.then(this.unloadPage.bind(this, pageID, false));
		}

		var prevState = this.state;
		var nextState = {
			pages: [].concat(prevState.pages),
			unloadedPageIDsTop: [].concat(prevState.unloadedPageIDsTop),
			unloadedPageIDsBottom: [].concat(prevState.unloadedPageIDsBottom),
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

	fetchPrevPage: function (wait) {
		if (wait !== false) {
			return this.wg.then(this.fetchPrevPage.bind(this, false));
		}

		var prevState = this.state;

		if (prevState.unloadedPageIDsTop.length === 0) {
			this.wg.addOne();
			Dispatcher.dispatch({
				name: 'FETCH_APP_HISTORY',
				direction: 'prev',
				appID: this.storeID.appID
			});
			this.allowNewPageTop = true;
			return;
		}

		var nextState = {};
		var pageIndex = prevState.unloadedPageIDsTop.length-1;
		nextState.unloadedPageIDsTop =prevState.unloadedPageIDsTop.slice(0, pageIndex);
		nextState.pages = prevState.allPages.slice(pageIndex, prevState.pages.length+pageIndex+1);
		nextState.nEvents = prevState.nEvents + nextState.pages[0].events.length;
		this.setState(nextState);
	},

	fetchNextPage: function (wait) {
		if (wait !== false) {
			return this.wg.then(this.fetchNextPage.bind(this, false));
		}

		var prevState = this.state;

		if (prevState.unloadedPageIDsBottom.length === 0) {
			this.wg.addOne();
			Dispatcher.dispatch({
				name: 'FETCH_APP_HISTORY',
				direction: 'next',
				appID: this.storeID.appID
			});
			this.allowNewPageBottom = true;
			return;
		}

		var nextState = {};
		var offset = prevState.unloadedPageIDsTop.length;
		nextState.unloadedPageIDsBottom = prevState.unloadedPageIDsBottom.slice(1);
		nextState.pages = prevState.allPages.slice(offset, prevState.pages.length+1+offset);
		nextState.nEvents = prevState.nEvents + nextState.pages[nextState.pages.length-1].events.length;
		this.setState(nextState);
	},

	__handleStoreChange: function () {
		if ( !this.wg.resolved ) {
			this.wg.removeOne();
		}

		var prevState = this.state;
		var nextState = {};
		var ahState = AppHistoryStore.getState(this.storeID);

		nextState.allPages = ahState.pages;
		nextState.nEvents = prevState.nEvents;

		if (prevState.pages.length === 0) {
			nextState.pages = ahState.pages.slice(0, DEFAULT_N_PAGES);
		} else if (ahState.pages.length !== prevState.allPages.length) {
			// pages are only added, and only to either end (never both)
			var offset = ahState.pages.length - prevState.allPages.length;
			if (this.allowNewPageBottom && prevState.allPages[0].id === ahState.pages[0].id && prevState.unloadedPageIDsBottom.length === 0) {
				// load one new page bottom
				this.allowNewPageBottom = false;
				nextState.pages = ahState.pages.slice(prevState.unloadedPageIDsTop.length, prevState.unloadedPageIDsTop.length + prevState.pages.length+1);
				nextState.nEvents = prevState.nEvents + nextState.pages[nextState.pages.length-1].events.length;
			} else if (this.allowNewPageTop && prevState.allPages[prevState.allPages.length-1].id === ahState.pages[ahState.pages.length-1].id && prevState.unloadedPageIDsTop.length === 0) {
				// load one new page top
				this.allowNewPageTop = false;
				nextState.pages = ahState.pages.slice(offset-1, prevState.pages.length+1);
				nextState.nEvents = prevState.nEvents + nextState.pages[0].events.length;
			}
		}

		var pages = nextState.pages || prevState.pages;
		if (prevState.unloadedPageIDsTop.length > 0 || (pages.length > 0 && pages[0].id !== nextState.allPages[0].id)) {
			nextState.hasPrevPage = true;
		} else {
			nextState.hasPrevPage = ahState.hasPrevPage;
		}
		if (prevState.unloadedPageIDsBottom.lenght > 0 || (pages.length > 0 && pages[pages.length-1].id !== nextState.allPages[nextState.allPages.length-1].id)) {
			nextState.hasNextPage = true;
		} else {
			nextState.hasNextPage = ahState.hasNextPage;
		}

		if (nextState.pages) {
			var delta = Math.abs(prevState.pages.length - nextState.pages.length);
		}

		this.setState(nextState);

		if (nextState.nEvents < 9 && nextState.hasPrevPage) {
			this.fetchPrevPage();
		}
	}
});

var Event = React.createClass({
	render: function () {
		var event = this.props.event;
		if (event.object_type === 'scale') {
			return this.renderScaleEvent(event);
		}
		if (event.object_type === 'app_release') {
			return this.renderReleaseEvent(event);
		}
		return (
			<div>
				Unsupported event type {event.object_type}
			</div>
		);
	},

	renderScaleEvent: function (event) {
		var diff = event.diff;
		var prevProcesses = event.data.prev_processes || {};
		return (
			<article>
				<div style={{display: 'flex'}}>
					<i className={event.delta >= 0 ? 'icn-up' : 'icn-down'} style={{marginRight: '0.5rem'}} />
					<ul style={{
						listStyle: 'none',
						padding: 0,
						margin: 0,
						display: 'flex'
					}}>
						{diff.map(function (d) {
							var delta;
							if (d.op === 'replace') {
								delta = d.value - (prevProcesses[d.key] || 0)
							}
							return (
								<li key={d.key} style={{
									padding: 0,
									marginRight: '1rem'
								}}>
									{d.op === 'add' ? (
										<span>{d.key}: {d.value}</span>
									) : null}
									{d.op === 'replace' ? (
										<span>{d.key}: {d.value} {delta !== 0 ? '('+(delta > 0 ? '+' : '')+delta+')' : null}</span>
									) : null}
									{d.op === 'remove' ? (
										<del>{d.key}</del>
									) : null}
								</li>
							);
						})}
					</ul>
				</div>
				<div>
					<Timestamp timestamp={event.created_at} />
				</div>
			</article>
		);
	},

	renderReleaseEvent: function (event) {
		return (
			<article>
				<div style={{display: 'flex'}}>
					<i className='icn-right' style={{marginRight: '0.5rem'}} />
					<div>
						Release {event.object_id}
					</div>
				</div>
				<ul style={{
					listStyle: 'none',
					padding: 0,
					margin: 0
				}}>
					{event.envDiff.map(function (d, i) {
						return (
							<li key={i} style={{
								padding: 0
							}}>
								{d.op === 'replace' || d.op === 'add' ? (
									<small>{d.key}: {d.value.length > 68 ? d.value.slice(0, 65) + '...' : d.value}</small>
								) : (
									<small><del>{d.key}</del></small>
								)}
							</li>
						);
					}, this)}
				</ul>
				<div>
					<Timestamp timestamp={event.created_at} />
				</div>
			</article>
		);
	}
});

var AppHistory = React.createClass({
	render: function () {
		var state = this.state;
		var even = false;

		return (
			<div className='app-history'>
				<header>
					<h2>App history</h2>
				</header>

				<section style={{position: 'relative', height: 300, overflowY: 'auto'}}>
					<ScrollPagination
						manager={this.props.scrollPaginationManager}
						hasPrevPage={this.state.hasPrevPage}
						hasNextPage={this.state.hasNextPage}
						unloadPage={this.__unloadPage}
						loadPrevPage={this.__fetchPrevPage}
						loadNextPage={this.__fetchNextPage}
						showNewItemsTop={true}>

						{state.pages.map(function (page) {
							return (
								<ScrollPagination.Page
									key={page.id}
									manager={this.props.scrollPaginationManager}
									id={page.id}
									component='ul'>

									{page.events.map(function (event) {
										var className = '';
										if (even) {
											className = 'even';
											even = false;
										} else {
											even = true;
										}
										return (
											<li key={event.id} className={className}>
												<Event event={event} />
											</li>
										);
									}, this)}
								</ScrollPagination.Page>
							);
						}, this)}

					</ScrollPagination>
				</section>
			</div>
		);
	},

	getDefaultProps: function () {
		return {
			scrollPaginationManager: new ScrollPagination.Manager()
		};
	},

	getInitialState: function () {
		return {
			pages: []
		};
	},

	componentDidMount: function () {
		this.paginator = new AppHistoryPaginator(this.props.appID);
		this.paginator.addChangeListener(this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		this.paginator.removeChangeListener(this.__handleStoreChange);
		this.paginator.close();
	},

	__handleStoreChange: function () {
		this.setState(this.paginator.getState());
	},

	__unloadPage: function (pageID) {
		this.paginator.unloadPage(pageID);
	},

	__fetchPrevPage: function () {
		this.paginator.fetchPrevPage();
	},

	__fetchNextPage: function () {
		this.paginator.fetchNextPage();
	}
});

export default AppHistory;
