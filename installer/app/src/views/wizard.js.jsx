import Config from '../config';
import InstallSteps from './install-steps';
import CloudSelector from './cloud-selector';
import CredentialsPicker from './credentials-picker';
import RouteLink from './route-link';
import AWSLauncher from './aws-launcher';
import DigitalOceanLauncher from './digital-ocean-launcher';
import AzureLauncher from './azure-launcher';
import SSHLauncher from './ssh-launcher';
import InstallProgress from './install-progress';
import Dashboard from './dashboard';
import Panel from './panel';
import Modal from './modal';
import FileInput from './file-input';
import Dispatcher from '../dispatcher';
import { default as BtnCSS, green as GreenBtnCSS } from './css/button';
import UserAgent from './css/user-agent';

var Wizard = React.createClass({
	render: function () {
		var cluster = this.state.currentCluster;
		if (cluster === null) {
			return <div />;
		}
		var state = cluster.state;
		return (
			<div style={{ height: '100%' }}>
				<div style={{
					display: UserAgent.isSafari() ? '-webkit-flex' : 'flex',
					flexFlow: 'column',
					WebkitFlexFlow: 'column',
					height: '100%'
				}}>
					{state.currentStep === 'configure' ? (
						<CloudSelector selectedCloud={this.state.currentCloudSlug} onChange={this.__handleCloudChange} />
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
					</Panel>
				</div>

				{state.prompt ? (
					<Modal visible={true} closable={false}>
						<header>
							{(function (lines) {
								if (lines.length === 1) {
									return <h2>{lines[0]}</h2>;
								} else {
									return (
										<div style={{
											marginBottom: 20,
											fontSize: '1.2rem'
										}}>
											{lines.map(function (line, i) {
												return <div key={i}>{line}</div>;
											})}
										</div>
									);
								}
							})(state.prompt.message.split('\n'))}
						</header>

						{(function (prompt) {
							switch (prompt.type) {
								case 'yes_no':
									return (
										<div>
											<button style={GreenBtnCSS} type="text" onClick={this.__handlePromptYesClick}>Yes</button>
											<button style={BtnCSS} type="text" onClick={this.__handlePromptNoClick}>No</button>
										</div>
									);
								case 'file':
									return (
										<form onSubmit={function(e){e.preventDefault();}}>
											<FileInput onChange={this.__handlePromptFileSelected} />
										</form>
									);
								case 'choice':
									return (
										<div>
											{prompt.options.map(function (option) {
												var css = BtnCSS;
												if (option.type === 1) {
													css = GreenBtnCSS;
												}
												return (
													<button key={option.value} style={css} onClick={function(e){
														e.preventDefault();
														this.__handleChoicePromptSelection(option.value);
													}.bind(this)}>{option.name}</button>
												);
											}.bind(this))}
										</div>
									);
							}
							return (
								<form onSubmit={this.__handlePromptInputSubmit}>
									<input ref="promptInput" type={prompt.type === 'protected_input' ? 'password' : 'text'} style={{
										width: 400,
										lineHeight: '1.5em',
										marginRight: '1em'
									}} />
									<button style={GreenBtnCSS} type="submit">Submit</button>
								</form>
							);
						}).call(this, state.prompt)}
					</Modal>
				) : null}
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

	componentDidUpdate: function () {
		if (this.state.currentCluster.state.prompt) {
			var el = this.refs.promptInput;
			if (el) {
				el.getDOMNode().focus();
			}
		}
	},

	__handleCloudChange: function (cloud) {
		Dispatcher.dispatch({
			name: 'SELECT_CLOUD',
			cloud: cloud,
			clusterID: this.state.currentCluster.attrs.ID
		});
	},

	__handleCredentialsChange: function (credentialID) {
		if (credentialID === 'new') {
			Dispatcher.dispatch({
				name: 'NAVIGATE',
				path: '/credentials',
				options: {
					params: [{
						cloud: this.state.currentCluster.state.selectedCloud
					}]
				}
			});
		} else {
			Dispatcher.dispatch({
				name: 'SELECT_CREDENTIAL',
				credentialID: credentialID,
				clusterID: this.state.currentCluster.attrs.ID
			});
		}
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

	__handlePromptFileSelected: function (file) {
		var reader = new FileReader();
		reader.onload = function () {
			this.__submitPromptResponse({
				input: btoa(reader.result)
			});
		}.bind(this);
		reader.readAsBinaryString(file);
	},

	__handlePromptInputSubmit: function (e) {
		e.preventDefault();
		var input = this.refs.promptInput.getDOMNode().value;
		if (this.state.currentCluster.state.prompt.type !== 'protected_input') {
			input = input.trim();
		}
		this.__submitPromptResponse({
			input: input
		});
	},

	__handleChoicePromptSelection: function (value) {
		this.__submitPromptResponse({
			input: value
		});
	},

	__submitPromptResponse: function (data) {
		Dispatcher.dispatch({
			name: 'INSTALL_PROMPT_RESPONSE',
			clusterID: this.state.currentClusterID,
			promptID: this.state.currentCluster.state.prompt.id,
			data: data
		});
	}
});
export default Wizard;
