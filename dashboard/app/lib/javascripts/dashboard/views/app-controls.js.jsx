/** @jsx React.DOM */
//= require ./app-processes
//= require ./app-resources
//= require ./app-routes
//= require ./route-link

(function () {

"use strict";

var RouteLink = Dashboard.Views.RouteLink;

Dashboard.Views.AppControls = React.createClass({
	displayName: "Views.AppControls",

	render: function () {
		var app = this.props.app;
		var formation = this.props.formation;
		var getAppPath = this.props.getAppPath;

		return (
			<section className="app-controls">
				<header>
					<h1>
						{app.name}
						<RouteLink path={getAppPath("/delete")}>
							<i className="icn-trash" />
						</RouteLink>
					</h1>
				</header>

				<section className="flex-row">
					<section className="col">
						<RouteLink path={getAppPath("/env")} className="btn-green">
							App environment
						</RouteLink>

						{formation ? (
							<Dashboard.Views.AppProcesses formation={formation} />
						) : (
							<section className="app-processes">
								&nbsp;
							</section>
						)}

						<RouteLink path={getAppPath("/logs")} className="logs-btn">
							Show logs
						</RouteLink>
					</section>

					<section className="col">
						<Dashboard.Views.AppResources
							appId={this.props.appId} />

						<Dashboard.Views.AppRoutes
							appId={this.props.appId}
							getAppPath={this.props.getAppPath} />
					</section>
				</section>
			</section>
		);
	}
});

})();
