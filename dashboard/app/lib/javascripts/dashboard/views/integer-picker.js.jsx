var IntegerPicker = React.createClass({
	displayName: "Views.IntegerPicker",

	render: function () {
		return (
			<div className={"integer-picker "+ (this.props.className || "")}>
				<div className="amount">{this.state.value}</div>
				{this.props.displayOnly ? null : (
					<ul className="controls">
						<li onClick={this.handleIncrementClick}>+</li>
						<li onClick={this.handleDecrementClick}>&#65112;</li>
					</ul>
				)}
			</div>
		);
	},

	getInitialState: function () {
		return {
			value: 0
		};
	},

	componentWillMount: function () {
		this.__setValue(this.props);
	},

	componentWillReceiveProps: function (props) {
		this.__setValue(props);
	},

	__setValue: function (props) {
		var value = props.value || 0;
		if (value !== this.state.value) {
			this.setState({
				value: value
			});
		}
	},

	__updateValue: function (delta) {
		var value = Math.max(this.state.value + delta, 0);
		if (value !== this.state.value) {
			var res = this.props.onChange(value);
			if (res === false) {
				return;
			}
			this.setState({
				value: value
			});
		}
	},

	handleIncrementClick: function () {
		this.__updateValue(1);
	},

	handleDecrementClick: function () {
		this.__updateValue(-1);
	}
});

export default IntegerPicker;
