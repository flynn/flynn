import BaseStore from 'marbles/store';
import Config from './config';

var Store = BaseStore.createClass({
	displayName: "Dashboard.Store",

	didBecomeInactive: function () {
		return Config.waitForRouteHandler.then(function () {
			if (this.__changeListeners.length === 0) {
				this.constructor.discardInstance(this);
			} else {
				return Promise.reject();
			}
		}.bind(this));
	}
});

export default Store;
