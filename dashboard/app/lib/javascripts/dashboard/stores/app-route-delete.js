import Store from 'marbles/store';
import Dispatcher from 'dashboard/dispatcher';

var AppRouteDelete = Store.createClass({
	willInitialize: function () {
		this.props = {
			appID: this.id.appID,
			routeID: this.id.routeID
		};
	},

	didBecomeInactive: function () {
		this.constructor.discardInstance(this);
	},

	getInitialState: function () {
		return {
			isDeleting: false,
			errorMsg: null
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'DELETE_APP_ROUTE':
			if (event.appID === this.props.appID && event.routeID === this.props.routeID) {
				this.setState({
					isDeleting: true,
					errorMsg: null
				});
			}
			break;

		case 'DELETE_APP_ROUTE_FAILED':
			if (event.appID === this.props.appID && event.routeID === this.props.routeID) {
				this.setState({
					errorMsg: 'Something went wrong ('+ event.status +')',
					isDeleting: false
				});
			}
			break;
		}
	}
});

AppRouteDelete.isValidId = function (id) {
	var keys = Object.keys(id || {});
	return keys.length === 2 && keys.indexOf('appID') !== -1 && keys.indexOf('routeID') !== -1;
};

AppRouteDelete.registerWithDispatcher(Dispatcher);

export default AppRouteDelete;
