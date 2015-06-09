import Dispatcher from '../dispatcher';

var AppDelete = {
	deleteApp: function (appId) {
		Dispatcher.handleViewAction({
			name: "APP_DELETE:DELETE_APP",
			storeId: {
				appId: appId
			}
		});
	}
};

export default AppDelete;
