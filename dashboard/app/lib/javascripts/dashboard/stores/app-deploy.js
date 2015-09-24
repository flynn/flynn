import Store from 'marbles/store';
import Dispatcher from 'dashboard/dispatcher';

var DEFAULT_ID = {appID: null};

var AppDeploy = Store.createClass({
	willInitialize: function () {
		this.props = {
			appID: this.id.appID,
			sha: this.id.sha
		};
	},

	didBecomeActive: function () {
		if (this.props.appID !== null) {
			Dispatcher.dispatch({
				name: 'GET_DEPLOY_APP_JOB',
				appID: this.props.appID
			});
			Dispatcher.dispatch({
				name: 'GET_APP_RELEASE',
				appID: this.props.appID
			});
		}
	},

	didBecomeInactive: function () {
		this.constructor.discardInstance(this);
	},

	getInitialState: function () {
		return {
			taffyJob: null,
			release: null,
			launching: this.props.appID !== null,
			launchFailed: null,
			launchErrorMsg: null,
			launchSuccess: null
		};
	},

	handleEvent: function (event) {
		switch (event.name) {
			case 'JOB':
				if (event.taffy !== true || this.props.appID === null || (event.data.meta || {}).app !== this.props.appID || event.data.meta.sha !== this.props.sha) {
					return;
				}
				if (this.state.taffyJob === null || event.object_id === this.state.taffyJob.id) {
					this.setState({
						taffyJob: event.data,
						launching: (event.data.state !== 'down' && event.data.state !== 'crashed'),
						launchFailed: event.data.state === 'crashed',
						launchSuccess: event.data.state === 'down',
						launchErrorMsg: event.data.state === 'crashed' ? 'Non-zero exit status': null
					});
					return;
				}
			break;

			case 'APP':
				if (event.app === this.props.appID && event.data.release) {
					Dispatcher.dispatch({
						name: 'GET_APP_RELEASE',
						appID: this.props.appID
					});
				}
			break;

			case 'APP_RELEASE':
				if (event.app === this.props.appID) {
					this.setState({
						release: event.data.release
					});
				}
			break;

			case 'UPDATE_APP_ENV_FAILED':
				if (event.appID === this.props.appID) {
					this.setState({
						launchErrorMsg: event.errorMsg
					});
				}
			break;
		}
	}
});

AppDeploy.isValidId = function (id) {
	if ( !id.hasOwnProperty('appID') ) { return false; }
	if ( !id.hasOwnProperty('sha') ) { return false; }
	return true;
};

AppDeploy.registerWithDispatcher(Dispatcher);

export { DEFAULT_ID };

export default AppDeploy;
