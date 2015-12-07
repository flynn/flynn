import Dispatcher from '../dispatcher';
import FileInput from './file-input';
import { default as BtnCSS, green as GreenBtnCSS } from './css/button';

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
				<FileInput onChange={this.__handleFileSelected} />
			</form>
		);
	},

	__handleFileSelected: function (file) {
		var reader = new FileReader();
		reader.onload = function () {
			this.props.onSubmit({
				input: btoa(reader.result)
			});
		}.bind(this);
		reader.readAsBinaryString(file);
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
