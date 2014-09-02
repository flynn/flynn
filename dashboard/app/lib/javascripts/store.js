(function () {
	"use strict";

	FlynnDashboard.Store = Marbles.Store.createClass({
		displayName: "FlynnDashboard.Store",

		didBecomeInactive: function () {
			return FlynnDashboard.waitForRouteHandler.then(function () {
				if (this.__changeListeners.length === 0) {
					this.constructor.discardInstance(this);
				} else {
					return Promise.reject();
				}
			}.bind(this));
		}
	});
})();
