import Router from 'marbles/router';
import WizardComponent from './views/wizard';
import Dispatcher from './dispatcher';

var MainRouter = Router.createClass({
	routes: [
		{ path: '', handler: 'landingPage' },
		{ path: '/install/:install_id', handler: 'landingPage' }
	],

	willInitialize: function () {
		Dispatcher.register(this.handleEvent.bind(this));
	},

	beforeHandler: function (event) {
		Dispatcher.dispatch({
			name: 'LOAD_INSTALL',
			id: event.params[0].install_id || ''
		});
	},

	landingPage: function (params, opts, context) {
		var props = {
			dataStore: context.dataStore
		};
		context.render(WizardComponent, props);
	},

	handleEvent: function (event) {
		var installID;
		switch (event.name) {
			case 'INSTALL_EXISTS':
				installID = this.history.pathParams[0].install_id;
				if ( !event.exists && (!event.id || event.id === installID) ) {
					this.history.navigate('/');
				} else if (event.exists && installID !== event.id) {
					this.history.navigate('/install/'+ event.id);
				}
			break;

			case 'LAUNCH_INSTALL_SUCCESS':
				installID = event.res.id;
				this.history.navigate('/install/'+ installID);
			break;
		}
	}
});
export default MainRouter;
