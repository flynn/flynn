FlynnDashboard.dispatcherIndex = FlynnDashboard.Dispatcher.register(
	FlynnDashboard.__handleEvent.bind(FlynnDashboard));

FlynnDashboard.config.fetch().catch(
		function(){}); // suppress SERVICE_UNAVAILABLE error
