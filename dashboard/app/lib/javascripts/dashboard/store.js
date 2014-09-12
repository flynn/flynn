(function () {
	"use strict";

	Dashboard.Store = Marbles.Store.createClass({
		displayName: "Dashboard.Store",

		didBecomeInactive: function () {
			return Dashboard.waitForRouteHandler.then(function () {
				if (this.__changeListeners.length === 0) {
					this.constructor.discardInstance(this);
				} else {
					return Promise.reject();
				}
			}.bind(this));
		}
	});
})();
