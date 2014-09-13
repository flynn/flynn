//= require ../store

(function () {

"use strict";

var AppResources = Dashboard.Stores.AppResources = Dashboard.Store.createClass({
	displayName: "Stores.AppResources",

	getState: function () {
		return this.state;
	},

	willInitialize: function () {
		this.props = {
			appId: this.id.appId
		};
	},

	didInitialize: function () {},

	didBecomeActive: function () {
		this.__fetchResources();
	},

	getInitialState: function () {
		return {
			resources: [],
			fetched: false
		};
	},

	handleEvent: function () {
	},

	__fetchResources: function () {
		return this.__getClient().getAppResources(this.props.appId).then(function (args) {
			var res = args[0];
			if (res === "null") {
				res = [];
			}
			this.setState({
				resources: res || [],
				fetched: true
			});
		}.bind(this));
	},

	__getClient: function () {
		return Dashboard.client;
	}

}, Marbles.State);

AppResources.isValidId = function (id) {
	return !!id.appId;
};

AppResources.registerWithDispatcher(Dashboard.Dispatcher);

})();
