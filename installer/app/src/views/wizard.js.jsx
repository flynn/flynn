import InstallSteps from './install-steps';
import AWSLauncher from './aws-launcher';
import InstallProgress from './install-progress';
import Dashboard from './dashboard';
import Modal from './modal';
import Dispatcher from '../dispatcher';
import { extend } from 'marbles/utils';
import { default as BtnCSS, green as GreenBtnCSS } from './css/button';

var Wizard = React.createClass({
	render: function () {
		var state = this.props.dataStore.state;
		return (
			<div>
				<InstallSteps state={state} />

				{state.currentStep === 'configure' ? (
					<AWSLauncher state={state} />
				) : null}

				{state.currentStep === 'install' ? (
					<InstallProgress state={state} />
				) : null}

				{state.currentStep === 'dashboard' ? (
					<Dashboard state={state} />
				) : null}

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
				) : null}
			</div>
		);
	},

	componentDidMount: function () {
		this.props.dataStore.addChangeListener(this.__handleDataChange);
	},

	componentWillUnmount: function () {
		this.props.dataStore.removeChangeListener(this.__handleDataChange);
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
			data: extend({}, data, {
				id: this.state.prompt.id
			})
		});
	}
});
export default Wizard;
