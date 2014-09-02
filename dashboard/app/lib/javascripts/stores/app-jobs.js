//= require ../store

(function () {

"use strict";

var AppJobs = FlynnDashboard.Stores.AppJobs = FlynnDashboard.Store.createClass({
	displayName: "Stores.AppJobs",

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
		this.__fetchJobs();
	},

	getInitialState: function () {
		return {
			processes: []
		};
	},

	handleEvent: function () {
	},

	__fetchJobs: function () {
		FlynnDashboard.client.getAppJobs(this.props.appId).then(function (args) {
			var res = args[0];
			this.setState({
				processes: res.map(function (item) {
					if (item.hasOwnProperty("State")) {
						item.state = item.State;
					}
					return item;
				})
			});
		}.bind(this));
	}

}, Marbles.State);

AppJobs.registerWithDispatcher(FlynnDashboard.Dispatcher);

})();
