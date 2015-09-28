import Store from 'marbles/store';
import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';
import { objectDiff } from 'dashboard/utils';
import { extend } from 'marbles/utils';

var buildPageID = function (events) {
	return events.map(function (e) { return e.id; }).join(':');
};

var getSinceID = function (pages) {
	return pages[0].events[0].id;
};

var getBeforeID = function (pages) {
	if (pages.length === 0) {
		return null;
	}
	var events = pages[pages.length-1].events;
	return events[events.length-1].id;
};

var OBJECT_TYPES = ['app_release', 'scale'];
var FETCH_COUNT = 3;

var AppHistory = Store.createClass({
	willInitialize: function () {
		this.props = {
			appID: this.id.appID,
		};

		this.fetchPageLock = Promise.resolve();
	},

	didBecomeActive: function () {
		if (this.state.pages.length === 0) {
			this.__fetchNextPage();
		}
	},

	didBecomeInactive: function () {
		this.constructor.discardInstance(this);
	},

	getInitialState: function () {
		return {
			hasPrevPage: true,
			hasNextPage: true,
			pages: [],
			eventIDs: []
		};
	},

	handleEvent: function (event) {
		if (event.app === this.props.appID && OBJECT_TYPES.indexOf(event.object_type) !== -1) {
			this.setState({
				hasPrevPage: true
			});
			return;
		}

		if (event.appID !== this.props.appID || event.name !== 'FETCH_APP_HISTORY') {
			return;
		}
		if (event.direction === 'prev') {
			this.__fetchPrevPage();
		} else {
			this.__fetchNextPage();
		}
	},

	__withFetchLock: function (callback) {
		return this.fetchPageLock.then(function () {
			this.fetchPageLock = new Promise(function (rs) {
				callback().then(rs);
			}.bind(this));
			return this.fetchPageLock;
		}.bind(this));
	},

	__fetchPrevPage: function (skipLock) {
		if (skipLock !== false) {
			return this.__withFetchLock(this.__fetchPrevPage.bind(this, false));
		}
		var prevState = this.state;
		return Config.client.getEvents({
			app_id: this.props.appID,
			since_id: getSinceID(prevState.pages),
			object_types: OBJECT_TYPES,
			count: FETCH_COUNT
		}).then(function (args) {
			var events = this.__rewriteEvents(this.__filterEvents(args[0]));
			var pages;
			var hasPrevPage = true;
			if (events.length === 0) {
				pages = prevState.pages;
				hasPrevPage = false;
			} else {
				pages = [{
					id: buildPageID(events),
					events: events
				}].concat(prevState.pages);
			}
			this.setState({
				hasPrevPage: hasPrevPage,
				pages: pages,
				eventIDs: prevState.eventIDs.concat(events.map(function (e) { return e.id; }))
			});
		}.bind(this)).catch(function (err) {
			setTimeout(function () {
				throw err;
			}, 0);
		});
	},

	__fetchNextPage: function (skipLock) {
		if (skipLock !== false) {
			return this.__withFetchLock(this.__fetchNextPage.bind(this, false));
		}
		var prevState = this.state;
		return Config.client.getEvents({
			app_id: this.props.appID,
			before_id: getBeforeID(prevState.pages),
			object_types: OBJECT_TYPES,
			count: FETCH_COUNT
		}).then(function (args) {
			var events = this.__rewriteEvents(this.__filterEvents(args[0]));
			var pages;
			var hasNextPage = true;
			if (events.length === 0) {
				pages = prevState.pages;
				hasNextPage = false;
			} else {
				pages = prevState.pages.concat([{
					id: buildPageID(events),
					events: events
				}]);
			}
			this.setState({
				hasNextPage: hasNextPage,
				pages: pages,
				eventIDs: prevState.eventIDs.concat(events.map(function (e) { return e.id; }))
			});
		}.bind(this)).catch(function (err) {
			setTimeout(function () {
				throw err;
			}, 0);
		});
	},

	__filterEvents: function (events) {
		return events.filter(function (event) {
			if (event.object_type === 'scale' && event.data.processes === null) {
				// don't show formation deletions
				return false;
			}
			if (event.object_type === 'scale' && !event.data.hasOwnProperty('prev_processes')) {
				// don't show scale events from deployments
				return false;
			}
			if (this.state.eventIDs.indexOf(event.id) !== -1) {
				// gaurd against duplicate events as this would break things
				return false;
			}
			return true;
		}, this);
	},

	__rewriteEvents: function (events) {
		return events.map(function (event) {
			if (event.object_type === 'app_release') {
				return this.__releaseEventWithDiff(event);
			}
			if (event.object_type === 'scale') {
				return this.__scaleEventWithDiff(event);
			}
			return event;
		}, this);
	},

	__releaseEventWithDiff: function (event) {
		var prevRelease = event.data.prev_release || {};
		var release = event.data.release;
		var envDiff = objectDiff(prevRelease.env || {}, release.env || {});
		return extend({}, event, {
			envDiff: envDiff
		});
	},

	__scaleEventWithDiff: function (event) {
		var prevProcesses = event.data.prev_processes || {};
		var processes = event.data.processes || {};
		var diff = objectDiff(prevProcesses, processes);
		var delta = 0;
		diff.forEach(function (d) {
			var k = d.key;
			delta += (processes[k] || 0) - (prevProcesses[k] || 0);
		});
		return extend({}, event, {
			delta: delta,
			diff: diff
		});
	}
});

AppHistory.isValidId = function (id) {
	return !!id.appID;
};

AppHistory.registerWithDispatcher(Dispatcher);

export default AppHistory;
