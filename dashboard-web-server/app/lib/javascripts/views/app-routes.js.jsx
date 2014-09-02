/** @jsx React.DOM */
//= require ../stores/app-routes
//= require ./external-link
//= require ./route-link

(function () {

"use strict";

var AppRoutesStore = FlynnDashboard.Stores.AppRoutes;

var ExternalLink = FlynnDashboard.Views.ExternalLink;
var RouteLink = FlynnDashboard.Views.RouteLink;

function getAppRoutesStoreId (props) {
	return {
		appId: props.appId
	};
}

function getState (props) {
	var state = {
		appStoreId: getAppRoutesStoreId(props)
	};

	var appRoutesState = AppRoutesStore.getState(state.appStoreId);
	state.routes = appRoutesState.routes;

	return state;
}

FlynnDashboard.Views.AppRoutes = React.createClass({
	displayName: "Views.AppRoutes",

	render: function () {
		var getAppPath = this.props.getAppPath;
		return (
			<section className="app-routes">
				<header>
					<h2>Domains</h2>
				</header>

				<ul>
					{this.state.routes.map(function (route) {
						return (
							<li key={route.id || route.config.domain}>
								<ExternalLink href={"http://"+ route.config.domain}>{route.config.domain}</ExternalLink>
								{route.id ? (
									<RouteLink path={getAppPath("/routes/:route/delete", {route: route.id, domain: route.config.domain})}>
										<i className="icn-trash" />
									</RouteLink>
								) : null}
							</li>
						);
					}, this)}
				</ul>

				<RouteLink path={getAppPath("/routes/new")}>
					<button className="add-route-btn" onClick={this.__handleAddRouteBtnClick}>Add new domain</button>
				</RouteLink>
			</section>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		AppRoutesStore.addChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppRoutesStoreId = this.state.appStoreId;
		var nextAppRoutesStoreId = getAppRoutesStoreId(nextProps);
		if ( !Marbles.Utils.assertEqual(prevAppRoutesStoreId, nextAppRoutesStoreId) ) {
			AppRoutesStore.removeChangeListener(prevAppRoutesStoreId, this.__handleStoreChange);
			AppRoutesStore.addChangeListener(nextAppRoutesStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		AppRoutesStore.removeChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props));
	}
});

})();
