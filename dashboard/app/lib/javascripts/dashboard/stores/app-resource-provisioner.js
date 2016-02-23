import Store from 'marbles/store';
import Dispatcher from 'dashboard/dispatcher';

var AppResourceProvisioner = Store.createClass({
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
			errMsg: null
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'APP_PROVISION_RESOURCES':
			this.setState({
				isCreating: true,
				errMsg: null
			});
			break;
		case 'APP_PROVISION_RESOURCE_FAILED':
			this.setState({
				isCreating: false,
				errMsg: event.errMsg
			});
			break;
		case 'DEPLOYMENT':
			if (event.app === this.props.appID) {
				if (event.data.status === 'failed') {
					this.setState({
						isCreating: false,
						errMsg: 'Error setting app env: '+ event.error
					});
				} else if (event.data.status === 'complete') {
					this.setState({
						isCreating: false,
						errMsg: null
					});
				}
			}
			break;
		}
	}
});

AppResourceProvisioner.registerWithDispatcher(Dispatcher);

export default AppResourceProvisioner;
