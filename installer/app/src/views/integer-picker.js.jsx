import UserAgent from './css/user-agent';
import LinearGradient from './css/linear-gradient';
import Colors from './css/colors';
import { extend } from 'marbles/utils';

var IntegerPicker = React.createClass({
	getDefaultProps: function () {
		return {
			minValue: 0,
			skipValues: [],
			maxValue: null,

			wrapperCSS: extend({
				display: UserAgent.isSafari() ? '-webkit-flex' : 'flex',
				flexFlow: 'row',
				WebkitFlexFlow: 'row',
				border: '1px solid '+ Colors.grayBlueColor,
				borderRadius: 4,
				boxShadow: '0px 0px 1px '+ Colors.grayBlueColor,
				fontWeight: 600
			}, LinearGradient('top', Colors.whiteColor, Colors.almostWhiteColor)),

			amountCSS: {
				display: 'block',
				flexGrow: 2,
				WebkitFlexGrow: 2,
				cursor: 'default',
				userSelect: 'none',
				MozUserSelect: 'none',

				textAlign: 'center',
				marginTop: '0.65em',
				paddingLeft: '0.35em',
				paddingRight: '0.35em'
			},

			controlsCSS: {
				display: 'flex',
				flexFlow: 'column',
				WebkitFlexFlow: 'column',
				flexGrow: 1,
				listStyle: 'none',
				padding: 0,
				margin: 0,
				color: Colors.darkerGrayBlueColor
			},

			controlCSS: {
				height: '50%',
				flexGrow: 1,
				WebkitFlexGrow: 1,
				borderLeft: '1px solid '+ Colors.grayBlueColor,
				borderBottom: '1px solid '+ Colors.grayBlueColor,
				textAlign: 'center',
				lineHeight: '1em',
				padding: '0.05em 0.25em',
				cursor: 'default',
				userSelect: 'none',
				MozUserSelect: 'none'
			}
		};
	},

	render: function () {
		return (
			<div style={this.props.wrapperCSS} className={"clearfix "+ (this.props.className || "")}>
				<div style={this.props.amountCSS}>{this.state.value}</div>
				{this.props.displayOnly ? null : (
					<ul style={this.props.controlsCSS}>
						<li style={this.props.controlCSS} onClick={this.handleIncrementClick}>+</li>
						<li style={extend(this.props.controlCSS, {borderBottom: 0})} onClick={this.handleDecrementClick}>&#65112;</li>
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
		var value = Math.max(this.state.value + delta, this.props.minValue);
		if (this.props.skipValues.indexOf(value) !== -1) {
			if (delta > 0) {
				this.__updateValue(delta + 1);
				return;
			} else {
				this.__updateValue(delta - 1);
				return;
			}
		}
		if (this.props.maxValue !== null && value > this.props.maxValue) {
			return;
		}
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
