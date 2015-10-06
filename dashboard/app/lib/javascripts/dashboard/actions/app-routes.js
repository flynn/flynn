import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';
import { extend } from 'marbles/utils';

var createAppRoute = function (appID, route) {
	var client = Config.client;
	client.getApp(appID).then(function (args) {
		var app = args[0];
		var data = extend({
			type: 'http',
			service: app.name + '-web'
		}, route);
		return client.createAppRoute(appID, data);
	});
};

var deleteAppRoute = function (appID, routeType, routeID) {
	var client = Config.client;
	client.deleteAppRoute(appID, routeType, routeID);
};

Dispatcher.register(function (event) {
	switch (event.name) {
	case 'CREATE_APP_ROUTE':
		createAppRoute(event.appID, event.data);
		break;

	case 'DELETE_APP_ROUTE':
		deleteAppRoute(event.appID, event.routeType, event.routeID);
		break;
	}
});
