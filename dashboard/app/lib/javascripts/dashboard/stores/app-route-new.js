import Store from 'marbles/store';
import Dispatcher from 'dashboard/dispatcher';

var AppRouteNew = Store.createClass({
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
			isCreating: false,
			routeDomain: null,
			errorMsg: null
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'CREATE_APP_ROUTE':
			if (event.appID === this.props.appID) {
				this.setState({
					errorMsg: null,
					isCreating: true,
					routeDomain: event.data.domain
				});
			}
			break;

		case 'ROUTE':
			if (event.app === this.props.appID && event.data.domain === this.state.routeDomain) {
				this.setState(this.getInitialState());
			}
			break;

		case 'CREATE_APP_ROUTE_FAILED':
			if (event.appID === this.props.appID && event.routeDomain === this.state.routeDomain) {
				this.setState({
					errorMsg: event.error ? event.error.message : 'Something went wrong ('+ event.status +')',
					isCreating: false
				});
			}
			break;
		}
	}
});

AppRouteNew.isValidId = function (id) {
	var keys = Object.keys(id || {});
	return keys.length === 1 && keys[0] === 'appID';
};

AppRouteNew.registerWithDispatcher(Dispatcher);

export default AppRouteNew;
