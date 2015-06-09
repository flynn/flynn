import Dispatcher from '../dispatcher';

var NewAppRoute = {
	createAppRoute: function (appId, domain) {
		Dispatcher.handleViewAction({
			name: "NEW_APP_ROUTE:CREATE_ROUTE",
			storeId: {
				appId: appId
			},
			domain: domain
		});
	}
};

export default NewAppRoute;
