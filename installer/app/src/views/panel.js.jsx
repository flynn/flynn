import Colors from './css/colors';
import { extend } from 'marbles/utils';

var Panel = React.createClass({
	getDefaultProps: function () {
		return {
			baseCSS: {
				backgroundColor: Colors.whiteColor,
				color: Colors.blackGrayColor,
				boxShadow: '0px 1px 2px '+ Colors.grayBlueColor,
				padding: 20
			}
		};
	},

	render: function () {
		var props = extend({}, this.props);
		delete props.style;
		delete props.baseCSS;
		return (
			<section style={extend({}, this.props.baseCSS, this.props.style || {})} {...props}>
				{this.props.children}
			</section>
		);
	}
});

export default Panel;
