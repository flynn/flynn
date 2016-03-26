import Store from 'marbles/store';
import Dispatcher from 'dashboard/dispatcher';

var ResourceAddApp = Store.createClass({
	willInitialize: function () {
		this.props = {
			appID: this.id.appID
		};
	},

	didBecomeInactive: function () {
		this.constructor.discardInstance(this);
	},

	getInitialState: function () {
		return {
			errMsg: null
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'RESOURCE_ADD_APP':
			this.setState({
				errMsg: null
			});
			break;
		case 'RESOURCE_ADD_APP_FAILED':
			this.setState({
				errMsg: event.errMsg
			});
			break;
		}
	}
});

ResourceAddApp.registerWithDispatcher(Dispatcher);

export default ResourceAddApp;
