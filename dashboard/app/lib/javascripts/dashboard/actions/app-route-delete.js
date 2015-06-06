import Dispatcher from '../dispatcher';

var AppRouteDelete = {
	deleteAppRoute: function (appId, routeType, routeId) {
		Dispatcher.handleViewAction({
			name: "APP_ROUTE_DELETE:DELETE_ROUTE",
			storeId: {
				appId: appId
			},
			routeType: routeType,
			routeId: routeId
		});
	}
};

export default AppRouteDelete;
