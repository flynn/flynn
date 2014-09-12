Dashboard.dispatcherIndex = Dashboard.Dispatcher.register(
	Dashboard.__handleEvent.bind(Dashboard));

Dashboard.config.fetch().catch(
		function(){}); // suppress SERVICE_UNAVAILABLE error
