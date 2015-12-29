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
				name: 'GET_APP',
				appID: this.props.appID
			});
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
			name: this.props.appID === null ? null : '',
			taffyJob: null,
			release: null,
			launching: this.props.appID !== null,
			launchFailed: null,
			launchErrorMsg: null,
			launchSuccess: null
		};
	},

	handleEvent: function (event) {
		var launchErrorMsg;
		switch (event.name) {
		case 'JOB':
			if (event.taffy !== true || this.props.appID === null || (event.data.meta || {}).app !== this.props.appID || event.data.meta.rev !== this.props.sha) {
				return;
			}
			if (this.state.taffyJob === null || event.object_id === this.state.taffyJob.uuid) {
				launchErrorMsg = (function (job) {
					if (job.host_error) {
						return job.host_error;
					}
					if (job.hasOwnProperty('exit_status') && job.exit_status !== 0) {
						return 'Non-zero exit status: '+ job.exit_status;
					}
					if (job.state === 'crashed') {
						return 'Non-zero exit status';
					}
					return null;
				})(event.data);
				this.setState({
					taffyJob: event.data,
					launching: (event.data.state !== 'down' && launchErrorMsg === null),
					launchFailed: launchErrorMsg !== null,
					launchSuccess: event.data.state === 'down' && launchErrorMsg === null,
					launchErrorMsg: launchErrorMsg
				});
				return;
			}
			break;

		case 'APP':
			if (event.app === this.props.appID) {
				this.setState({
					name: event.data.name
				});
				if (event.data.release) {
					Dispatcher.dispatch({
						name: 'GET_APP_RELEASE',
						appID: this.props.appID
					});
				}
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
