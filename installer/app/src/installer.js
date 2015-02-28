import MainRouter from './router';
import History from 'marbles/history';
import Dispatcher from './dispatcher';
import MainStore from './main-store';
import MainComponent from './views/main';

var dataStore = new MainStore();
dataStore.registerWithDispatcher(Dispatcher);

var history = new History();
history.register(new MainRouter());

export default {
	run: function (el) {
		this.el = el;
		this.dispatcherIndex = Dispatcher.register(this.__handleEvent.bind(this));

		this.dataStore = dataStore;

		history.start({
			context: this,
			dispatcher: Dispatcher
		});
	},

	render: function (component, props, children) {
		props.key = 'content';
		var contentComponent = React.createElement(component, props, children);
		React.render(
			React.createElement(MainComponent, {}, [contentComponent]),
			this.el
		);
	},

	__handleEvent: function (event) {
		if (event.source === "Marbles.History") {
			switch (event.name) {
				case "handler:before":
					this.__handleHandlerBeforeEvent(event);
				break;

				case "handler:after":
					this.__handleHandlerAfterEvent(event);
				break;
			}
			return;
		}
	},

	__handleHandlerBeforeEvent: function () {
		window.scrollTo(0,0);
		this.primaryView = null;
		React.unmountComponentAtNode(this.el);
	},

	__handleHandlerAfterEvent: function () {
	},
};
