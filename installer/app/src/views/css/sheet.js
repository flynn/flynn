import UserAgent from './user-agent';
import CSS from 'css';

var webkitFlexTransformer = function (field, value) {
	switch (field) {
		case 'display':
			if (value === 'flex' && UserAgent.isSafari()) {
				return [field, '-webkit-flex'];
			}
		break;

		case 'flexGrow':
			if (UserAgent.isSafari()) {
				return ['WebkitFlexGrow', value];
			}
		break;

		case 'flexBasis':
			if (UserAgent.isSafari()) {
				return ['WebkitFlexBasis', value];
			}
		break;
	}
	return [field, value];
};

var sheet = new CSS({
	transformers: [webkitFlexTransformer]
});
export default sheet;
