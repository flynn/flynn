import Dispatcher from '../dispatcher';

var Nav = {
	handleAuthBtnClick: function () {
		Dispatcher.handleViewAction({
			name: "AUTH_BTN_CLICK"
		});
	}
};

export default Nav;
