import { aruba } from 'grommet-theme-aruba';
import tinycolor from 'tinycolor2';

const colors = {
	green: '#1bb45e',
	blue: '#08a1f4',
	orangeLight: '#ff7700',
	gray: '#727272',
	black: '#000000',
	white: '#ffffff'
};
const modifiedAruba = Object.assign({}, aruba, {
	global: Object.assign({}, (aruba as any).global, {
		font: {
			family: null // inherit from ./index.css
		},
		colors: Object.assign({}, (aruba as any).global.colors, {
			// color used on active hover state
			active:
				'#' +
				tinycolor(colors.white)
					.darken(10)
					.toHex(),
			// the main brand color
			brand: colors.green,
			// the color to be used when element is in focus
			focus: colors.blue,
			// the text color of the input placeholder
			placeholder: colors.gray,
			// shade of white
			white: colors.white,
			// shade of black
			black: colors.black,
			'accent-1':
				'#' +
				tinycolor(colors.gray)
					.darken(20)
					.toHex(),
			'status-warning': colors.orangeLight,
			textInput: {
				backgroundColor: colors.white
			},
			border: {
				// default border color for light mode
				light: colors.gray,
				// default border color for dark mode
				dark: colors.white
			},
			control: {
				// default control color for light mode
				light: colors.green,
				// default control color for dark mode
				dark: colors.green
			},
			text: {
				// the default application text color for light mode
				light: colors.gray,
				// the default application text color for dark mode
				dark: colors.white
			}
		})
	})
});
export default modifiedAruba;
