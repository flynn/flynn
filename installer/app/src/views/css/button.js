import Colors from './colors';
import { extend } from 'marbles/utils';

var buttonCSS = {
	display: 'inline-block',
	position: 'relative',
	borderRdius: 4,
	cursor: 'pointer',
	fontSize: '1em',
	fontWeight: 400,
	lineHeight: '1em',
	padding: '0.75em 1em',
	textDecoration: 'none',
	backgroundColor: Colors.almostWhiteColor,
	color: Colors.blackColor,
	border: 0
};

var green = extend({}, buttonCSS, {
	backgroundColor: Colors.greenColor,
	color: Colors.almostWhiteColor
});

var disabled = {
	opacity: 0.6,
	cursor: 'not-allowed'
};

export { green, disabled };
export default buttonCSS;
