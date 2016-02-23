import { extend } from 'marbles/utils';
import Modal from 'Modal';
import Dispatcher from 'dashboard/dispatcher';
import AppStore from 'dashboard/stores/app';
import AppResourcesStore from 'dashboard/stores/app-resources';
import AppResourceProvisionerStore from 'dashboard/stores/app-resource-provisioner';
import ProvidersStore from 'dashboard/stores/providers';
import ProviderPicker from 'dashboard/views/provider-picker';

var providersStoreID = 'default';
var appStoreID = function (props) {
	return {
		appId: props.appID
	};
};
var appResourcesStoreID = appStoreID;
var appResourceProvisionerStoreID = function (props) {
	return {
		appID: props.appID
	};
};

var AppResourceProvisioner = React.createClass({
	displayName: "Views.AppResourceProvisioner",

	render: function () {
		var newResourcesState = this.state.newResourcesState;
		var selectedProviderIDs = this.state.selectedProviderIDs;
		var disabledProviderIDs = this.state.disabledProviderIDs;
		var app = this.state.app;

		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section>
					<header>
						<h1>Provision database(s)</h1>
					</header>

					<p className="alert-info">Select the databases you would like to provision{app ? ' for '+ app.name : ''}.</p>

					{this.state.errMsg ? (
						<p className="alert-error">{this.state.errMsg}</p>
					) : null}

					<ProviderPicker
						disabledProviderIDs={disabledProviderIDs}
						onChange={this.__handleProvidersChange} />

					{newResourcesState.hasError ? (
						<ul style={{
							listStyle: 'none',
							padding: 0,
							margin: 0,
							marginTop: '1rem',
							marginBottom: '1rem'
						}}>
							{newResourcesState.errMsgs.map(function (errMsg, index) {
								return (
									<li key={selectedProviderIDs[index]} className="alert-error">
										{errMsg}
									</li>
								);
							}, this)}
						</ul>
					) : null}

					<button disabled={newResourcesState.isCreating || selectedProviderIDs.length === 0} className="btn-green btn-block" onClick={this.__handleProvisionBtnClick}>
						{newResourcesState.isCreating ? ( "Please wait..." ) : (
							"Provision database"+ (selectedProviderIDs.length > 1 ? 's' : '')
						)}
					</button>
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	__getState: function (props, newState) {
		var prevState = this.state || {};
		var state =  extend({}, prevState, {
			selectedProviderIDs: []
		}, newState);

		var newResourcesState = {
			isCreating: false,
			hasError: false,
			errMsgs: []
		};
		var providersState = ProvidersStore.getState(providersStoreID);
		state.selectedProviderIDs.forEach(function (providerID) {
			var rs = providersState.newResourceStates[providerID] || {
				isCreating: null,
				errMsg: null
			};
			if (rs.errMsg) {
				newResourcesState.errMsgs.push(rs.errMsg);
			}
		});
		newResourcesState.hasError = newResourcesState.errMsgs.length > 0;
		state.newResourcesState = newResourcesState;

		var appResourcesState = AppResourcesStore.getState(appResourcesStoreID(props));
		var disabledProviderIDs = [];
		if (appResourcesState.fetched) {
			disabledProviderIDs = appResourcesState.resources.map(function (resource) {
				return resource.provider;
			});
		} else {
			// disable them all until we know which ones are already provisioned for this app
			disabledProviderIDs = providersState.providers.map(function (provider) {
				return provider.id;
			});
		}
		state.disabledProviderIDs = disabledProviderIDs;

		state.app = AppStore.getState(appStoreID(props)).app;

		var appResourceProvisionerState = AppResourceProvisionerStore.getState(appResourceProvisionerStoreID(props));
		state.errMsg = appResourceProvisionerState.errMsg;
		state.newResourcesState.isCreating = appResourceProvisionerState.isCreating;

		return state;
	},

	componentDidMount: function () {
		AppStore.addChangeListener(appStoreID(this.props), this.__handleStoreChange);
		AppResourcesStore.addChangeListener(appResourcesStoreID(this.props), this.__handleStoreChange);
		AppResourceProvisionerStore.addChangeListener(appResourceProvisionerStoreID(this.props), this.__handleStoreChange);
		ProvidersStore.addChangeListener(providersStoreID, this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		AppStore.removeChangeListener(appStoreID(this.props), this.__handleStoreChange);
		AppResourcesStore.removeChangeListener(appResourcesStoreID(this.props), this.__handleStoreChange);
		AppResourceProvisionerStore.removeChangeListener(appResourceProvisionerStoreID(this.props), this.__handleStoreChange);
		ProvidersStore.removeChangeListener(providersStoreID, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props));
	},

	__handleProvidersChange: function (providerIDs) {
		this.setState(this.__getState(this.props, {
			selectedProviderIDs: providerIDs
		}));
	},

	__handleProvisionBtnClick: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'APP_PROVISION_RESOURCES',
			providerIDs: this.state.selectedProviderIDs,
			appID: this.props.appID
		});
	}
});

export default AppResourceProvisioner;
