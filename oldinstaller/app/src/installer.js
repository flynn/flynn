import MainRouter from './router';
import History from 'marbles/history';
import Dispatcher from './dispatcher';
import MainStore from './main-store';
import MainComponent from './views/main';
import Client from './client';

var dataStore = new MainStore(Dispatcher);

var history = new History();
history.register(new MainRouter());

export default {
	run: function (el, modalEl) {
		this.el = el;
		this.modalEl = modalEl;
		this.dispatcherIndex = Dispatcher.register(this.__handleEvent.bind(this));

		this.dataStore = dataStore;

		history.start({
			context: this,
			dispatcher: Dispatcher
		});

		Client.openEventStream();
	},

	render: function (component, props, children) {
		props.key = 'content';
		var contentComponent = React.createElement(component, props, children);
		React.render(
			React.createElement(MainComponent, { dataStore: this.dataStore }, [contentComponent]),
			this.el
		);
	},

	renderModal: function (component, props, children) {
		React.render(
			React.createElement(component, props, children),
			this.modalEl
		);
	},

	unRenderModal: function () {
		React.unmountComponentAtNode(this.modalEl);
	},

	__handleEvent: function (event) {
		if (event.source === "Marbles.History") {
			switch (event.name) {
			case "handler:before":
				this.__handleHandlerBeforeEvent(event);
				break;
			}
		}
	},

	__handleHandlerBeforeEvent: function () {
		window.scrollTo(0,0);
		this.primaryView = null;
		React.unmountComponentAtNode(this.el);
	}
};
