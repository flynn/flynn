(function () {

"use strict";

Dashboard.Dispatcher = Marbles.Utils.extend({
	handleViewAction: function (action) {
		this.dispatch(Marbles.Utils.extend({
			source: "VIEW_ACTION"
		}, action));
	},

	handleStoreEvent: function (event) {
		this.dispatch(Marbles.Utils.extend({
			source: "STORE_EVENT"
		}, event));
	},

	handleAppEvent: function (event) {
		this.dispatch(Marbles.Utils.extend({
			source: "APP_EVENT"
		}, event));
	}
}, Marbles.Dispatcher);

})();
