/** @jsx React.DOM */

Flynn.Views.NameField = React.createClass({
	displayName: "Flynn.Views.NameField",

	getInitialState: function () {
		return {
			valid: null,
			msg: null
		};
	},

	getDefaultProps: function () {
		return {
			validationRegex: /^.+$/
		};
	},

	handleValueUpdated: function (newValue) {
		this.props.handleValuesUpdated({
			name: newValue
		});
	},

	performValidation: function (value, showError, callbacks) {
		if (this.props.validationRegex.test(value)) {
			this.setState({
				valid: true,
				msg: null
			});
			if (callbacks) {
				callbacks.success();
			}
		} else if (showError) {
			this.setState({
				valid: false,
				msg: "Please enter you name."
			});
			if (callbacks) {
				callbacks.failure();
			}
		}
	},

	render: function () {
		var InputField = Flynn.Views.InputField;

		return (
			<InputField
				type="text"
				name={this.props.name}
				label={this.props.label}
				placeholder={this.props.placeholder}
				valid={this.state.valid}
				msg={this.state.msg}
				performValidation={this.performValidation}
				handleValueUpdated={this.handleValueUpdated}
				initialValue={this.props.initialValue}
			/>
		);
	}
});
