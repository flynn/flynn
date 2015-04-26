import Colors from './css/colors';
import { extend } from 'marbles/utils';
import RouteLink from './route-link';

var List = React.createClass({
	getDefaultProps: function () {
		return {
			baseCSS: {
				listStyle: 'none',
				margin: 0,
				padding: 0
			}
		};
	},

	render: function () {
		return (
			<ul style={extend({}, this.props.baseCSS, this.props.style || {})}>
				{this.props.children}
			</ul>
		);
	}
});

var ListItem = React.createClass({
	getDefaultProps: function () {
		return {
			baseCSS: {
				padding: '0.5em 1em',
			},

			selectedCSS: {
				backgroundColor: Colors.greenColor,
				color: Colors.whiteColor
			}
		};
	},

	getCSS: function () {
		return extend({},
			this.props.baseCSS,
			this.props.selected ? this.props.selectedCSS : {},
			this.props.style || {}
		);
	},

	render: function () {
		var wrappedChildren = this.props.children;
		var css = this.getCSS();
		if (this.props.path) {
			wrappedChildren = (
				<RouteLink
					path={this.props.path}
					params={this.props.params || [{}]}
					style={extend({
						color: 'inherit',
						textDecoration: 'none',
						padding: css.padding,
						display: 'block'
					}, this.props.innerStyle || {})}>
					{this.props.children}
				</RouteLink>
			);
			css.padding = 0;
		} else {
			extend(css, this.props.innerStyle || {});
		}
		return (
			<li style={css}>
				{wrappedChildren}
			</li>
		);
	}
});

export { List, ListItem };
