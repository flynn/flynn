import Dispatcher from '../dispatcher';

var AppProcesses = {
	createFormation: function (appId, formation) {
		Dispatcher.handleViewAction({
			name: "APP_PROCESSES:CREATE_FORMATION",
			storeId: {
				appId: appId
			},
			formation: formation
		});
	}
};

export default AppProcesses;
