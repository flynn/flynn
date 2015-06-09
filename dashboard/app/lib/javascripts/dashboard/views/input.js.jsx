var Input = React.createClass({
	displayName: "Views.Input",

	getInitialState: function () {
		return {
			changing: false
		};
	},

	handleInputChange: function () {
		this.setState({
			changing: true
		});
		this.props.valueLink.requestChange(this.refs.input.getDOMNode().value);
	},

	handleInputBlur: function () {
		this.setState({
			changing: false
		});
	},

	// called from the outside world
	setChanging: function (changing) {
		this.setState({
			changing: changing
		});
	},

	// called from the outside world
	focus: function () {
		this.refs.input.getDOMNode().focus();
	},

	render: function () {
		var valid = this.props.valueLink.validation.valid;
		var msg = this.props.valueLink.validation.msg;

		if (this.state.changing && valid === false) {
			valid = null;
			msg = null;
		}

		if (valid === false && this.props.required !== true && !this.props.valueLink.value) {
			valid = null;
			msg = null;
		}

		return (
			<label>
				{this.props.label ? (
					<span className="text">
						{this.props.label}
					</span>
				) : null}
				<div className={"input-append"+ (valid === true ? " valid" : (valid === false ? " invalid" : ""))}>
					<input
						ref="input"
						type={this.props.type}
						value={this.props.valueLink.value}
						autoComplete={this.props.autoComplete}
						size={this.props.size}
						disabled={this.props.disabled}
						pattern={this.props.pattern}
						onChange={this.handleInputChange}
						onBlur={this.handleInputBlur} />
					<i className="addon" title={msg} />
					{this.props.children}
				</div>
				{msg ? (
					<div className="info">{msg}</div>
				) : null}
			</label>
		);
	}
});

export default Input;
