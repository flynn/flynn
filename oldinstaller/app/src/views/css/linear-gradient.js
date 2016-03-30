import UserAgent from './user-agent';

var vendorBackground = function (startPosition, startColor, endColor) {
	if (UserAgent.isFirefox()) {
		return '-moz-linear-gradient('+ startPosition +', '+ startColor +', '+ endColor +')';
	} else {
		return '-webkit-linear-gradient('+ startPosition +', '+ startColor +', '+ endColor +')';
	}
};

export default function (startPosition, startColor, endColor) {
	return {
		backgroundColor: startColor,
		background: vendorBackground(startPosition, startColor, endColor)
	};
}
