import Dispatcher from '../dispatcher';
import Config from '../config';

var AppAuth = {
	setGithubToken: function (token) {
		// TODO(jvatic): Use Dispatcher for this
		Config.setGithubToken(token);
	},

	createRelease: function (storeId, release) {
		Dispatcher.handleViewAction({
			name: "APP_ENV:CREATE_RELEASE",
			storeId: storeId,
			release: release
		});
	}
};

export default AppAuth;
