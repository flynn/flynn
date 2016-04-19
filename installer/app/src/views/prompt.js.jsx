import { extend } from 'marbles/utils';
import Config from '../config';
import Dispatcher from '../dispatcher';
import FileInput from './file-input';
import CredentialsPicker from './credentials-picker';
import { default as BtnCSS, green as GreenBtnCSS, disabled as DisabledBtnCSS } from './css/button';

var YesNoPrompt = React.createClass({
	render: function () {
		return (
			<div>
				<button style={GreenBtnCSS} type="text" onClick={this.__handleYesClick}>Yes</button>
				<button style={BtnCSS} type="text" onClick={this.__handleNoClick}>No</button>
			</div>
		);
	},

	__handleYesClick: function (e) {
		e.preventDefault();
		this.props.onSubmit({
			yes: true
		});
	},

	__handleNoClick: function (e) {
		e.preventDefault();
		this.props.onSubmit({
			yes: false
		});
	}
});

var FilePrompt = React.createClass({
	render: function () {
		return (
			<form onSubmit={function(e){e.preventDefault();}}>
				<FileInput onChange={this.__handleFileSelected} style={{
					marginBottom: '1em'
				}} />
			</form>
		);
	},

	__handleFileSelected: function (file) {
		this.props.onSubmit({
			type: "file",
			file: file
		});
	}
});

var ChoicePrompt = React.createClass({
	render: function () {
		var prompt = this.props.prompt;
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
							this.__handleSelection(option.value);
						}.bind(this)}>{option.name}</button>
					);
				}.bind(this))}
			</div>
		);
	},

	__handleSelection: function (value) {
		this.props.onSubmit({
			input: value
		});
	}
});

var CredentialPrompt = React.createClass({
	render: function () {
		var clusterState = this.props.state.currentCluster.state;
		return (
			<form onSubmit={this.__handleSubmit}>
				<CredentialsPicker
					style={{
						display: 'inline',
						marginRight: '1rem'
					}}
					credentials={clusterState.credentials}
					value={this.state.credentialID}
					onChange={this.__handleCredentialsChange}>
					<option value="new">New</option>
					{clusterState.selectedCloud === 'aws' && Config.has_aws_env_credentials ? (
						<option value="aws_env">Use AWS Env vars ({Config.aws_env_credentials_id})</option>
					) : null}
				</CredentialsPicker>
				<button
					type="submit"
					style={extend({}, GreenBtnCSS, this.state.submitDisabled ? DisabledBtnCSS : {})}
					disabled={this.state.submitDisabled}>Save</button>
			</form>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentWillReceiveProps: function (props) {
		var clusterState = props.state.currentCluster.state;
		var clusterID = props.state.currentClusterID;
		if (clusterState.credentialID !== this.state.initialCredentialID || props.state.credentialsPromptValue[clusterID] !== this.state.initialCredentialID) {
			this.setState(this.__getState(props, this.state));
		}
	},

	__getState: function (props, prevState) {
		prevState = prevState || {};
		var clusterState = props.state.currentCluster.state;
		var clusterID = props.state.currentClusterID;
		var state = {
			initialCredentialID: clusterState.credentialID,
			credentialID: props.state.credentialsPromptValue[clusterID] || clusterState.credentialID
		};
		return this.__computeState(state);
	},

	__computeState: function (state) {
		state = extend({}, this.state, state);
		state.submitDisabled = state.initialCredentialID === state.credentialID;
		return state;
	},

	__handleCredentialsChange: function (credentialID) {
		var clusterState = this.props.state.currentCluster.state;
		var clusterID = this.props.state.currentClusterID;
		if (credentialID === 'new') {
			Dispatcher.dispatch({
				name: 'NAVIGATE',
				path: '/credentials',
				options: {
					params: [{
						cloud: clusterState.selectedCloud,
						cluster_id: clusterID
					}]
				}
			});
		} else {
			this.setState(this.__computeState({
				credentialID: credentialID
			}));
		}
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		this.props.onSubmit({
			input: this.state.credentialID
		});
	}
});

var InputPrompt = React.createClass({
	render: function () {
		var prompt = this.props.prompt;
		return (
			<form onSubmit={this.__handleSubmit}>
				<input ref="input" type={prompt.type === 'protected_input' ? 'password' : 'text'} style={{
					width: 400,
					lineHeight: '1.5em',
					marginRight: '1em'
				}} />
				<button style={GreenBtnCSS} type="submit">Submit</button>
			</form>
		);
	},

	componentDidMount: function () {
		this.refs.input.getDOMNode().focus();
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		var input = this.refs.input.getDOMNode().value;
		if (this.props.prompt.type !== 'protected_input') {
			input = input.trim();
		}
		this.props.onSubmit({
			input: input
		});
	}
});

var Prompt = React.createClass({
	render: function () {
		var prompt = this.props.prompt;
		return (
			<section>
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
					})(prompt.message.split('\n'))}
				</header>

				{(function (prompt) {
					var Component = InputPrompt;
					switch (prompt.type) {
					case 'yes_no':
						Component = YesNoPrompt;
						break;
					case 'file':
						Component = FilePrompt;
						break;
					case 'choice':
						Component = ChoicePrompt;
						break;
					case 'credential':
						Component = CredentialPrompt;
						break;
					}
					return <Component prompt={prompt} state={this.props.state} onSubmit={this.__handlePromptSubmit} />;
				}).call(this, prompt)}
			</section>
		);
	},

	__handlePromptSubmit: function (res) {
		Dispatcher.dispatch({
			name: 'INSTALL_PROMPT_RESPONSE',
			clusterID: this.props.state.currentClusterID,
			promptID: this.props.prompt.id,
			data: res
		});
	}
});

export default Prompt;
