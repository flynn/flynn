import BaseDispatcher from 'marbles/dispatcher';
import { extend } from 'marbles/utils';

var Dispatcher = extend({
	handleViewAction: function (action) {
		this.dispatch(extend({
			source: "VIEW_ACTION"
		}, action));
	},

	handleStoreEvent: function (event) {
		this.dispatch(extend({
			source: "STORE_EVENT"
		}, event));
	},

	handleAppEvent: function (event) {
		this.dispatch(extend({
			source: "APP_EVENT"
		}, event));
	}
}, BaseDispatcher);

export default Dispatcher;
