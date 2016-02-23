import RouteLink from './route-link';

var AppsListHeader = React.createClass({
	displayName: "Views.AppsListHeader",

	render: function () {
		return (
			<section className="clearfix">
				<RouteLink
					className="btn-green float-right"
					path="/github">
						{this.props.githubAuthed ? (
							"Create application"
						) : (
							<span className="connect-with-github">
								<i className="icn-github-mark" />
								Connect with Github
							</span>
						)}
				</RouteLink>

				<RouteLink
					className="btn-green float-right"
					path="/providers"
					style={{ marginRight: '1rem' }}>
						Provision database
				</RouteLink>
			</section>
		);
	}
});

export default AppsListHeader;
