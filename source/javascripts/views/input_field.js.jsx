/** @jsx React.DOM */

Flynn.Views.InputField = React.createClass({
	displayName: "Flynn.Views.InputField",

	getInitialState: function () {
		return {
			valid: null,
			msg: null
		};
	},

	componentDidMount: function () {
		if (this.props.initialValue) {
			this.performValidation(this.props.initialValue, false);
		}
	},

	componentWillReceiveProps: function (props) {
		if (props.hasOwnProperty('valid')) {
			this.setState({ valid: props.valid });
		}
		if (props.hasOwnProperty('msg')) {
			this.setState({ msg: props.msg });
		}
	},

	handleChange: function (e) {
		this.props.handleValueUpdated(null);
		clearTimeout(this.__handleChangeValidationTimeout);
		this.__handleChangeValidationTimeout = setTimeout(this.__handleChangeValidation, 60);
	},

	__handleChangeValidation: function (showError) {
		clearTimeout(this.__handleChangeValidationTimeout);

		var value = this.refs.input.getDOMNode().value;

		this.performValidation(value, showError || false, {
			success: function () {
				this.props.handleValueUpdated(value);
			}.bind(this),
			failure: function () {
				this.props.handleValueUpdated(null);
			}.bind(this)
		});
	},

	handleBlur: function (e) {
		this.__handleChangeValidation(true);
	},

	performValidation: function (value, showError, callbacks) {
		if (value === "") {
			this.setState({
				msg: null,
				valid: null
			});
		}

		this.props.performValidation(value, showError, callbacks);
	},

	// called from the outside world
	focusInput: function () {
		this.refs.input.getDOMNode().focus();
	},

	render: function () {
		var valid = this.state.valid;

		var msg;
		if (this.state.msg) {
			msg = <div className="info">{this.state.msg}</div>;
		}

		var inputAttrs = Marbles.Utils.extend({}, this.props, {
			ref: "input",
			defaultValue: this.props.initialValue,
			onChange: this.handleChange,
			onBlur: this.handleBlur
		});
		delete inputAttrs.valid;
		delete inputAttrs.msg;
		delete inputAttrs.children;
		delete inputAttrs.label;
		var input = React.DOM.input(inputAttrs);

		var addons = this.props.children;
		var labelText;
		if (this.props.label) {
			labelText = <span className="text">{this.props.label}</span>;
		}

		return (
			<label>
				{labelText}
				<div className={"input-append"+ (valid === true ? " valid" : (valid === false ? " invalid" : ""))}>
					{input}
					{addons}
					<i className="addon" />
				</div>
				{msg}
			</label>
		);
	}
});
