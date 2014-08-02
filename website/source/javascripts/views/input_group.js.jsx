/** @jsx React.DOM */

Flynn.Views.InputGroup = React.createClass({
	displayName: "Flynn.Views.InputGroup",

	render: function () {
		var label;
		if (this.props.label) {
			label = (
				<label>
					<div className="text">{this.props.label}</div>
				</label>
			);
		}
		return (
			<div className="input-group">
				{label}
				{this.props.children}
			</div>
		);
	},
});
