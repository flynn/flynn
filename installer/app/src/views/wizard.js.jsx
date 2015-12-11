import Config from '../config';
import InstallSteps from './install-steps';
import CloudSelector from './cloud-selector';
import CredentialsPicker from './credentials-picker';
import Cluster from './cluster';
import RouteLink from './route-link';
import AWSLauncher from './aws-launcher';
import DigitalOceanLauncher from './digital-ocean-launcher';
import AzureLauncher from './azure-launcher';
import SSHLauncher from './ssh-launcher';
import InstallProgress from './install-progress';
import Dashboard from './dashboard';
import Panel from './panel';
import Dispatcher from '../dispatcher';
import { default as BtnCSS } from './css/button';
import UserAgent from './css/user-agent';

var Wizard = React.createClass({
	render: function () {
		var cluster = this.props.dataStore.state.currentCluster;
		if (cluster === null) {
			return <div />;
		}
		var state = cluster.state;
		return (
			<div>
				<div style={{
					display: UserAgent.isSafari() ? '-webkit-flex' : 'flex',
					flexFlow: 'column',
					WebkitFlexFlow: 'column',
					height: '100%'
				}}>
					{state.currentStep === 'configure' ? (
						<CloudSelector selectedCloud={this.props.dataStore.state.currentCloudSlug} onChange={this.__handleCloudChange} />
					) : null}

					<InstallSteps state={state} style={{ height: 16 }} />

					<Panel style={{ flexGrow: 1, WebkitFlexGrow: 1 }}>
						{state.currentStep === 'configure' && state.selectedCloud !== 'ssh' ? (
							state.credentialID ? (
								<CredentialsPicker
									credentials={state.credentials}
									value={state.credentialID}
									onChange={this.__handleCredentialsChange}>
									<option value="new">New</option>
									{state.selectedCloud === 'aws' && Config.has_aws_env_credentials ? (
										<option value="aws_env">Use AWS Env vars ({Config.aws_env_credentials_id})</option>
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

						{state.currentStep === 'configure' && state.selectedCloud === 'azure' && state.credentialID ? (
							<AzureLauncher state={state} />
						) : null}

						{state.currentStep === 'configure' && state.selectedCloud === 'ssh' ? (
							<SSHLauncher state={state} />
						) : null}

						{state.currentStep === 'install' ? (
							<InstallProgress state={state} />
						) : null}

						{state.currentStep === 'dashboard' ? (
							<Dashboard state={state} clusterID={cluster.attrs.ID} />
						) : null}

						{state.currentStep === 'done' ? (
							<Cluster state={state} clusterID={cluster.attrs.ID} />
						) : null}
					</Panel>
				</div>
			</div>
		);
	},

	getInitialState: function () {
		return this.props.dataStore.state;
	},

	__handleCloudChange: function (cloud) {
		Dispatcher.dispatch({
			name: 'SELECT_CLOUD',
			cloud: cloud,
			clusterID: this.props.dataStore.state.currentCluster.attrs.ID
		});
	},

	__handleCredentialsChange: function (credentialID) {
		if (credentialID === 'new') {
			Dispatcher.dispatch({
				name: 'NAVIGATE',
				path: '/credentials',
				options: {
					params: [{
						cloud: this.props.dataStore.state.currentCluster.state.selectedCloud
					}]
				}
			});
		} else {
			Dispatcher.dispatch({
				name: 'SELECT_CREDENTIAL',
				credentialID: credentialID,
				clusterID: this.props.dataStore.state.currentCluster.attrs.ID
			});
		}
	},

	__handleAbortBtnClick: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'INSTALL_ABORT'
		});
	}
});
export default Wizard;
