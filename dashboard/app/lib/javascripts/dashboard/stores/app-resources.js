import State from 'marbles/state';
import Store from '../store';
import Dispatcher from '../dispatcher';

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
		Dispatcher.dispatch({
			name: 'GET_APP_RESOURCES',
			appID: this.props.appId
		});
	},

	getInitialState: function () {
		return {
			resources: [],
			fetched: false
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'RESOURCE':
			if (event.app === this.props.appId) {
				this.setStateWithDelay({
					resources: this.state.resources.filter(function (r) {
						return r.id !== event.object_id;
					}).concat([event.data])
				});
			}
			break;

		case 'RESOURCE_DELETED':
			if (event.app === this.props.appId) {
				this.setStateWithDelay({
					resources: this.state.resources.filter(function (r) {
						return r.id !== event.object_id;
					})
				});
			}
			break;

		case 'APP_RESOURCE_REMOVED':
			if (event.appID === this.props.appId) {
				this.setStateWithDelay({
					resources: this.state.resources.filter(function (r) {
						return r.id !== event.resourceID;
					})
				});
			}
			break;

		case 'APP_RESOURCES_FETCHED':
			if (event.appID === this.props.appId) {
				this.setStateWithDelay({
					fetched: true
				});
			}
			break;
		}
	}

}, State);

AppResources.isValidId = function (id) {
	return !!id.appId;
};

AppResources.registerWithDispatcher(Dispatcher);

export default AppResources;
