import Colors from './css/colors';

var Panel = React.createClass({
	getDefaultProps: function () {
		return {
			css: {
				backgroundColor: Colors.whiteColor,
				color: Colors.blackGrayColor,
				boxShadow: '0px 1px 2px '+ Colors.grayBlueColor,
				padding: 20
			}
		};
	},

	render: function () {
		return (
			<section style={this.props.css}>
				{this.props.children}
			</section>
		);
	}
});

export default Panel;
