import AppProcesses from './app-processes';
import AppResources from './app-resources';
import AppRoutes from './app-routes';
import RouteLink from './route-link';

var AppControls = React.createClass({
	displayName: "Views.AppControls",

	render: function () {
		var app = this.props.app;
		var formation = this.props.formation;
		var getAppPath = this.props.getAppPath;
		var headerComponent = this.props.headerComponent || "header";

		return (
			<section className="app-controls">
				{React.createElement(headerComponent, this.props,
					<h1>
						{app.name}
						<RouteLink path={getAppPath("/delete")}>
							<i className="icn-trash" />
						</RouteLink>
					</h1>
				)}

				<section>
					<RouteLink path={getAppPath("/env")} className="btn-green">
						App environment
					</RouteLink>

					{formation ? (
						<AppProcesses appId={this.props.appId} formation={formation} />
					) : (
						<section className="app-processes">
							&nbsp;
						</section>
					)}

					<RouteLink path={getAppPath("/logs")} className="logs-btn">
						Show logs
					</RouteLink>
				</section>

				<section>
					<AppResources
						appId={this.props.appId}
						getAppPath={this.props.getAppPath} />

					<AppRoutes
						appId={this.props.appId}
						getAppPath={this.props.getAppPath} />
				</section>
			</section>
		);
	}
});

export default AppControls;
