import Config from '../config';
import InstallSteps from './install-steps';
import CloudSelector from './cloud-selector';
import CredentialsPicker from './credentials-picker';
import RouteLink from './route-link';
import AWSLauncher from './aws-launcher';
import DigitalOceanLauncher from './digital-ocean-launcher';
import InstallProgress from './install-progress';
import Dashboard from './dashboard';
import Panel from './panel';
import Modal from './modal';
import Dispatcher from '../dispatcher';
import { default as BtnCSS, green as GreenBtnCSS } from './css/button';
import UserAgent from './css/user-agent';

var Wizard = React.createClass({
	render: function () {
		var cluster = this.state.currentCluster;
		if (cluster === null) {
			return <div />;
		}
		var state = cluster.getInstallState();
		return (
			<div style={{ height: '100%' }}>
				<div style={{
					display: UserAgent.isSafari() ? '-webkit-flex' : 'flex',
					flexFlow: 'column',
					WebkitFlexFlow: 'column',
					height: '100%'
				}}>
					{state.currentStep === 'configure' ? (
						<CloudSelector state={state} onChange={this.__handleCloudChange} />
					) : null}

					<InstallSteps state={state} style={{ height: 16 }} />

					<Panel style={{ flexGrow: 1, WebkitFlexGrow: 1 }}>
						{state.currentStep === 'configure' ? (
							state.credentialID ? (
								<CredentialsPicker
									credentials={state.credentials}
									value={state.credentialID}
									onChange={this.__handleCredentialsChange}>
									{state.selectedCloud === 'aws' && Config.has_aws_env_credentials ? (
										<option value="aws_env">Use AWS Env vars</option>
									) : null}
								</CredentialsPicker>
							) : (
								<RouteLink path={'/credentials?cloud='+ state.selectedCloud} style={BtnCSS}>Add credentials to continue</RouteLink>
							)) : null}

						{state.currentStep === 'configure' && state.selectedCloud === 'aws' && state.credentialID ? (
							<AWSLauncher state={state} />
						) : null}

						{state.currentStep === 'configure' && state.selectedCloud === 'digital_ocean' && state.credentialID ? (
							<DigitalOceanLauncher state={state} />
						) : null}

						{state.currentStep === 'install' ? (
							<InstallProgress state={state} />
						) : null}

						{state.currentStep === 'dashboard' ? (
							<Dashboard state={state} clusterID={cluster.ID} />
						) : null}
					</Panel>
				</div>

				{state.prompt ? (
					<Modal visible={true} closable={false}>
						<header>
							<h2>{state.prompt.message}</h2>
						</header>

						{state.prompt.type === 'yes_no' ? (
							<div>
								<button style={GreenBtnCSS} type="text" onClick={this.__handlePromptYesClick}>Yes</button>
								<button style={BtnCSS} type="text" onClick={this.__handlePromptNoClick}>No</button>
							</div>
						) : (
							<form onSubmit={this.__handlePromptInputSubmit}>
								<input ref="promptInput" type="text" style={{
									width: 400,
									lineHeight: '1.5em',
									marginRight: '1em'
								}} />
								<button style={GreenBtnCSS} type="submit">Submit</button>
							</form>
						)}
					</Modal>
				) : (state.failed && !state.errorDismissed ? (
					<Modal visible={true} onHide={this.__handleFailedModalHide}>
						<header>
							<h2>Install failed</h2>
						</header>

						<p>{state.errorMessage}</p>
					</Modal>
				) : null)}
			</div>
		);
	},

	getInitialState: function () {
		return this.props.dataStore.state;
	},

	componentDidMount: function () {
		this.props.dataStore.addChangeListener(this.__handleDataChange);
	},

	componentWillUnmount: function () {
		this.props.dataStore.removeChangeListener(this.__handleDataChange);
	},

	__handleCloudChange: function (cloud) {
		Dispatcher.dispatch({
			name: 'SELECT_CLOUD',
			cloud: cloud,
			clusterID: this.state.currentCluster.ID
		});
	},

	__handleCredentialsChange: function (credentialID) {
		Dispatcher.dispatch({
			name: 'SELECT_CREDENTIAL',
			credentialID: credentialID,
			clusterID: this.state.currentCluster.ID
		});
	},

	__handleFailedModalHide: function () {
		Dispatcher.dispatch({
			name: 'INSTALL_ERROR_DISMISS',
			clusterID: this.state.currentCluster.ID
		});
	},

	__handleAbortBtnClick: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'INSTALL_ABORT'
		});
	},

	__handleDataChange: function () {
		this.setState(this.props.dataStore.state);
	},

	__handlePromptYesClick: function (e) {
		e.preventDefault();
		this.__submitPromptResponse({
			yes: true
		});
	},

	__handlePromptNoClick: function (e) {
		e.preventDefault();
		this.__submitPromptResponse({
			yes: false
		});
	},

	__handlePromptInputSubmit: function (e) {
		e.preventDefault();
		this.__submitPromptResponse({
			input: this.refs.promptInput.getDOMNode().value.trim()
		});
	},

	__submitPromptResponse: function (data) {
		Dispatcher.dispatch({
			name: 'INSTALL_PROMPT_RESPONSE',
			clusterID: this.state.currentClusterID,
			promptID: this.state.currentCluster.getInstallState().prompt.id,
			data: data
		});
	}
});
export default Wizard;
