var Main = React.createClass({
	getDefaultProps: function () {
		return {
			css: {
				margin: 16
			}
		};
	},

	render: function () {
		return (
			<div style={this.props.css}>
				{this.props.children}
			</div>
		);
	}
});
export default Main;
