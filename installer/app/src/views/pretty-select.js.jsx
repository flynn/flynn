import LinearGradient from './css/linear-gradient';
import Colors from './css/colors';
import { extend } from 'marbles/utils';

var PrettySelect = React.createClass({
	getDefaultProps: function () {
		return {
			wrapperCSS: extend({
				border: '1px solid '+ Colors.grayBlueColor,
				boxShadow: '0px 0px 1px'+ Colors.grayBlueColor,
				borderRadius: 2,

				position: 'relative',
				display: 'inline-block',
				width: 172,
				left: 'auto',
				bottom: 'auto'
			}, LinearGradient('top', Colors.whiteColor, Colors.almostWhiteColor)),

			wrapperAfterCSS: {
				display: 'block',
				position: 'absolute',
				top: 11,
				right: 8,
				width: 0,
				height: 0,
				borderLeft: '5px solid transparent',
				borderRight: '5px solid transparent',
				borderTop: '5px solid '+ Colors.darkerGrayBlueColor,
				pointerEvents: 'none'
			},

			selectCSS: {
				width: 173,
				padding: '4px 8px',
				paddingRight: 22,
				background: 'transparent',
				color: 'rgba(0,0,0,0)',
				textShadow: '0 0 0 '+ Colors.darkerGrayBlueColor,
				border: 'none',
				textOverflow: 'ellipsis',
				textIndent: 0.1,
				MozAppearance: 'radio-container',
				WebkitAppearance: 'none',
				boxShadow: 'inset 0px 0px 1px '+ Colors.grayBlueColor,
				outline: 0
			}
		};
	},

	render: function () {
		var selectProps = {
			style: this.props.selectCSS,
			onChange: this.props.onChange,
			defaultValue: this.props.defaultValue
		};
		if (this.props.hasOwnProperty('value')) {
			selectProps.value = this.props.value;
		}
		return (
			<div style={this.props.wrapperCSS}>
				<select {...selectProps}>

					{this.props.children}
				</select>

				<div style={this.props.wrapperAfterCSS}>&nbsp;</div>
			</div>
		);
	}
});
export default PrettySelect;
