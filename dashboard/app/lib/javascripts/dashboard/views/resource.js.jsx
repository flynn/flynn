import { extend } from 'marbles/utils';
import Config from 'dashboard/config';
import Dispatcher from 'dashboard/dispatcher';
import AppsStore from 'dashboard/stores/apps';
import AppStore from 'dashboard/stores/app';
import AppRoutesStore from 'dashboard/stores/app-routes';
import ResourceAddAppStore from 'dashboard/stores/resource-add-app';
import RouteLink from 'dashboard/views/route-link';
import ResourceRoute from 'dashboard/views/resource-route';
import EditEnv from 'dashboard/views/edit-env';

var appsStoreID = null;
var appRoutesStoreID = function (props) {
	return {
		appId: props.resourceApp ? props.resourceApp.id : props.providerApp.id,
		routeTypes: ['tcp']
	};
};
var resourceAddAppStoreID = 'default';

var providerAttrs = Config.PROVIDER_ATTRS;

var isSystemApp = AppStore.isSystemApp;

var Resource = React.createClass({
	displayName: "Views.Resource",

	render: function () {
		var appsByID = this.state.appsByID;
		var showAdvanced = this.state.showAdvanced;
		var hasExternalRoute = this.state.hasExternalRoute;
		var isProviderExternalRoute = !this.props.resourceApp;
		var isAddingApp = this.state.isAddingApp;

		if ( !appsByID ) {
			return <div />;
		}

		var provider = this.props.provider;
		var resource = this.props.resource;
		var pAttrs = providerAttrs[provider.name];
		var apps = resource.apps || [];
		var isSystemResource = false;
		var appNames = apps.map(function (appID) {
			var app = appsByID[appID];
			if (isSystemApp(app)) {
				isSystemResource = true;
			}
			return app.name;
		});
		var allOtherApps = this.state.apps.filter(function (app) {
			return !(resource.apps || []).find(function (appID) {
				return appID === app.id;
			});
		});
		return (
			<section className='resource'>
				<header>
					<h1>
						{pAttrs.title} ({appNames.length ? appNames.join(', ') : 'Standalone'})
						{isSystemResource ? null : (
							<RouteLink path={'/providers/'+ resource.provider +'/resources/'+ resource.id +'/delete'}>
								<i className="icn-trash" />
							</RouteLink>
						)}
					</h1>
				</header>

				<section>
					<ResourceRoute hasExternal={hasExternalRoute} cmds={this.state.routeCmds} urls={this.state.routeURLs} />
					{!hasExternalRoute && !apps.length ? (
						<button className="btn-green" onClick={this.__handleCreateRouteBtnClick} style={{marginBottom: '1rem'}}>Create external route{isProviderExternalRoute ? ' for provider' : ''}</button>
					) : null}
				</section>

				<section>
					<button className="btn" onClick={this.__toggleShowAdvanced} style={{marginBottom: '1rem'}}>{showAdvanced ? 'Hide' : 'Show'} advanced settings</button>
					<EditEnv
						env={resource.env}
						disabled={true}
						style={{
							textAlign: 'left',
							display: showAdvanced ? 'block' : 'none'
						}} />
				</section>

				<section className='resource-apps'>
					<h2>Apps</h2>
					<ul>
						{apps.map(function (appID) {
							return (
								<li key={appID}>
									<RouteLink path={'/apps/'+ appID}>{appsByID[appID].name}</RouteLink>
									<RouteLink className="delete-link" path={'/providers/'+ resource.provider +'/resources/'+ resource.id +'/apps/'+ appID +'/delete'}>
										<i className="icn-trash" />
									</RouteLink>
								</li>
							);
						}, this)}
					</ul>

					{isSystemResource ? null : (
						<section className="resource-add-app" style={{ marginTop: '1rem' }}>
							{this.state.addAppErrMsg ? (
								<p className="alert-error">{this.state.addAppErrMsg}</p>
							) : null}

							<select onChange={this.__handleAddAppIDChange} value={this.state.addAppID}>
								<option></option>
								{allOtherApps.map(function (app) {
									return (
										<option key={app.id} value={app.id}>{app.name}</option>
									);
								})}
							</select>

							<br/>

							<button
								className="btn-green"
								disabled={isAddingApp || !this.state.addAppID}
								onClick={this.__handleAddAppBtnClick}
								style={{marginTop: '1rem' }}>{isAddingApp ? 'Please wait...' : 'Add to application'}</button>
						</section>
					)}
				</section>
			</section>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props, {});
	},

	componentDidMount: function () {
		AppRoutesStore.addChangeListener(appRoutesStoreID(this.props), this.__handleStoreChange);
		AppsStore.addChangeListener(appsStoreID, this.__handleStoreChange);
		ResourceAddAppStore.addChangeListener(resourceAddAppStoreID, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		this.setState(this.__getState(nextProps, this.state));
	},

	componentWillUnmount: function () {
		AppRoutesStore.removeChangeListener(appRoutesStoreID(this.props), this.__handleStoreChange);
		AppsStore.removeChangeListener(appsStoreID, this.__handleStoreChange);
		ResourceAddAppStore.removeChangeListener(resourceAddAppStoreID, this.__handleStoreChange);
	},

	__handleCreateRouteBtnClick: function (e) {
		e.preventDefault();
		Config.history.navigate('/providers/'+ this.props.provider.id +'/resources/'+ this.props.resource.id +'/create-external-route', { replace: true });
	},

	__toggleShowAdvanced: function (e) {
		e.preventDefault();
		this.setState(this.__getState(this.props, this.state, {showAdvanced: !this.state.showAdvanced}));
	},

	__handleAddAppIDChange: function (e) {
		var appID = e.target.value;
		this.setState(this.__getState(this.props, this.state, {addAppID: appID}));
	},

	__handleAddAppBtnClick: function (e) {
		e.preventDefault();
		this.setState(this.__getState(this.props, this.state, {isAddingApp: true}));
		Dispatcher.dispatch({
			name: 'RESOURCE_ADD_APP',
			appID: this.state.addAppID,
			providerID: this.props.provider.id,
			resourceID: this.props.resource.id
		});
	},

	__getState: function (props, prevState, newState) {
		var state = extend({
			showAdvanced: false,
			addAppID: null,
			isAddingApp: false
		}, prevState, newState || {});

		// check to see if app ID has been added to resource
		if ((props.resource.apps || []).find(function (appID) { return appID === state.addAppID; })) {
			state.addAppID = null;
			state.isAddingApp = false;
		}

		var appsState = AppsStore.getState(appsStoreID);
		var appsByID = {};
		appsState.apps.forEach(function (app) {
			appsByID[app.id] = app;
		});
		state.appsByID = appsState.fetched ? appsByID : null;
		state.apps = appsState.apps.filter(function (app) {
			return !isSystemApp(app);
		});

		var appRoutesState = AppRoutesStore.getState(appRoutesStoreID(props));
		var routes = appRoutesState.fetched ? appRoutesState.routes : null;
		var externalRoute = (routes || []).find(function (route) {
			return route.leader === true && route.type === 'tcp';
		});
		state.hasExternalRoute = externalRoute === undefined && !appRoutesState.fetched ? null : !!externalRoute;

		var pAttrs = providerAttrs[props.provider.name];
		state.routeCmds = {
			name: pAttrs.cmd.name,
			external: Config.expandTemplateStr(pAttrs.cmd.externalTemplate, extend({}, props.resource.env, {
				defaultRouteDomain: Config.default_route_domain,
				port: externalRoute ? externalRoute.port : ''
			})),
			internal: Config.expandTemplateStr(pAttrs.cmd.internalTemplate, props.resource.env)
		};
		state.routeURLs = {
			external: Config.expandTemplateStr(pAttrs.uri.externalTemplate, extend({}, props.resource.env, {
				defaultRouteDomain: Config.default_route_domain,
				port: externalRoute ? externalRoute.port : ''
			})),
			internal: Config.expandTemplateStr(pAttrs.uri.internalTemplate, props.resource.env)
		};

		state.addAppErrMsg = ResourceAddAppStore.getState(resourceAddAppStoreID).errMsg;
		if (state.addAppErrMsg) {
			state.isAddingApp = false;
		}

		return state;
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props, this.state));
	}
});

export default Resource;
