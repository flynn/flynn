import Panel from './panel';
import InstallCert from './install-cert';
import Dispatcher from '../dispatcher';

var InstallProgress = React.createClass({
	render: function () {
		return (
			<Panel>
				<form ref="form" method="POST" action={"https://dashboard."+ this.state.domain +"/user/sessions"} onSubmit={this.__handleFormSubmit}>
					<input type="hidden" name="token" value={this.state.dashboardLoginToken} />
					<InstallCert certURL={"data:application/x-x509-ca-cert;base64,"+ this.state.cert} />
				</form>
			</Panel>
		);
	},

	getInitialState: function () {
		return this.__getState();
	},

	componentDidMount: function () {
		if (this.state.certVerified) {
			this.refs.form.getDOMNode().submit();
		} else {
			window.addEventListener("focus", this.__handleWindowFocus, false);
		}
	},

	componentWillUnmount: function () {
		window.removeEventListener("focus", this.__handleWindowFocus);
	},

	componentWillReceiveProps: function () {
		this.setState(this.__getState());
	},

	componentDidUpdate: function () {
		if (this.state.certVerified) {
			this.refs.form.getDOMNode().submit();
		}
	},

	__getState: function () {
		return this.props.state;
	},

	__handleWindowFocus: function () {
		Dispatcher.dispatch({
			name: 'CHECK_CERT'
		});
	},

	__handleFormSubmit: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'CHECK_CERT'
		});
	}
});
export default InstallProgress;
