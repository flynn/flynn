import Store from 'marbles/store';
import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';
import { objectDiff } from 'dashboard/utils';
import { extend, assertEqual } from 'marbles/utils';

var buildPageID = function (events) {
	return events.map(function (e) { return e.id; }).join(':');
};

var OBJECT_TYPES = ['app_release', 'scale'];
var FETCH_COUNT = 3;

var AppHistory = Store.createClass({
	willInitialize: function () {
		this.props = {
			appID: this.id.appID
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
			eventIDs: [],
			beforeID: null,
			sinceID: null
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
		if (skipLock !== true) {
			return this.__withFetchLock(this.__fetchPrevPage.bind(this, true));
		}
		var prevState = this.state;
		return Config.client.getEvents({
			app_id: this.props.appID,
			since_id: this.state.sinceID,
			object_types: OBJECT_TYPES,
			count: FETCH_COUNT
		}).then(function (args) {
			var events = this.__rewriteEvents(this.__filterEvents(args[0]));
			var pages;
			var hasPrevPage = true;
			if (events.length === 0) {
				pages = prevState.pages;
				hasPrevPage = args[0].length !== 0;
			} else {
				pages = [{
					id: buildPageID(events),
					events: events
				}].concat(prevState.pages);
			}
			this.setState({
				hasPrevPage: hasPrevPage,
				pages: pages,
				eventIDs: prevState.eventIDs.concat(events.map(function (e) { return e.id; })),
				sinceID: ((args[0] || [])[0] || {}).id || prevState.sinceID
			});
		}.bind(this)).catch(function (err) {
			setTimeout(function () {
				throw err;
			}, 0);
		});
	},

	__fetchNextPage: function (skipLock, beforeID, eventsBuffer) {
		if (skipLock !== true) {
			return this.__withFetchLock(this.__fetchNextPage.bind(this, true));
		}
		if (this.state.hasNextPage === false) {
			return Promise.resolve();
		}
		var prevState = this.state;
		return Config.client.getEvents({
			app_id: this.props.appID,
			before_id: beforeID || this.state.beforeID,
			object_types: OBJECT_TYPES,
			count: FETCH_COUNT
		}).then(function (args) {
			var events = (eventsBuffer || []).concat(this.__rewriteEvents(this.__filterEvents(args[0])));
			var pages;
			var hasNextPage = true;
			var beforeID = ((args[0] || [])[args[0].length-1] || {}).id || prevState.beforeID;
			if (args[0].length > events.length) {
				return this.__fetchNextPage(skipLock, beforeID, events);
			}
			if (events.length === 0) {
				pages = prevState.pages;
				hasNextPage = args[0].length !== 0;
			} else {
				pages = prevState.pages.concat([{
					id: buildPageID(events),
					events: events
				}]);
			}
			var state = {
				hasNextPage: hasNextPage,
				pages: pages,
				eventIDs: prevState.eventIDs.concat(events.map(function (e) { return e.id; })),
				beforeID: ((args[0] || [])[args[0].length-1] || {}).id || prevState.beforeID
			};
			this.setState(state);
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
			if (event.object_type === 'scale' && assertEqual(event.data.processes, event.data.prev_processes)) {
				// don't show scale events in which nothing's changed
				return false;
			}
			if (this.state.eventIDs.indexOf(event.id) !== -1) {
				// guard against duplicate events as this would break things
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
