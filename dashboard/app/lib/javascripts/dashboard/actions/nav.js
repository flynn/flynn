import Config from 'dashboard/config';
import Dispatcher from '../dispatcher';

var Nav = {
	handleAuthBtnClick: function () {
		if (Config.isNavFrozen) {
			return;
		}
		Dispatcher.handleViewAction({
			name: "AUTH_BTN_CLICK"
		});
	}
};

export default Nav;
