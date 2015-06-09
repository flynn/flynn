import State from 'marbles/state';
import Store from '../store';
import Dispatcher from '../dispatcher';
import Config from '../config';

var AppResources = Store.createClass({
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
		return Config.client;
	}

}, State);

AppResources.isValidId = function (id) {
	return !!id.appId;
};

AppResources.registerWithDispatcher(Dispatcher);

export default AppResources;
