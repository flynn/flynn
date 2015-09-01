import Dispatcher from 'dashboard/dispatcher';
import Config from 'dashboard/config';

var deleteApp = function (appID) {
	var client = Config.client;
	client.deleteApp(appID);
};

Dispatcher.register(function (event) {
	if (event.name === 'DELETE_APP') {
		deleteApp(event.appID);
	}
});
