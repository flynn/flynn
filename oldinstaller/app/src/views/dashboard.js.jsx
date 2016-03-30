import InstallCert from './install-cert';
import Dispatcher from '../dispatcher';
import { green as GreenBtnCSS } from './css/button';
import Config from '../config';
import UserAgent from './css/user-agent';

var InstallProgress = React.createClass({
	render: function () {
		return (
			<form ref="form" method="POST" action={"https://dashboard."+ this.state.domainName +"/user/sessions"} onSubmit={this.__handleFormSubmit}>
				<input type="hidden" name="token" value={this.state.dashboardLoginToken} />
				{this.state.certVerified ? (
					<button type="submit" style={GreenBtnCSS}>Go to Dashboard</button>
				) : (
					<InstallCert certURL={UserAgent.isSafari() ? Config.endpoints.cert.replace(":id", this.props.clusterID) : ("data:application/x-x509-ca-cert;base64,"+ this.state.caCert)} />
				)}
			</form>
		);
	},

	getInitialState: function () {
		return this.__getState();
	},

	componentDidMount: function () {
		window.addEventListener("focus", this.__handleWindowFocus, false);
	},

	componentWillUnmount: function () {
		window.removeEventListener("focus", this.__handleWindowFocus);
	},

	componentWillReceiveProps: function () {
		this.setState(this.__getState());
	},

	__getState: function () {
		return this.props.state;
	},

	__handleWindowFocus: function () {
		Dispatcher.dispatch({
			name: 'CHECK_CERT',
			clusterID: this.props.clusterID,
			domainName: this.state.domainName
		});
	},

	__handleFormSubmit: function (e) {
		if ( !this.state.certVerified ) {
			e.preventDefault();
			Dispatcher.dispatch({
				name: 'CHECK_CERT',
				clusterID: this.props.clusterID,
				domainName: this.state.domainName
			});
		}
	}
});
export default InstallProgress;
