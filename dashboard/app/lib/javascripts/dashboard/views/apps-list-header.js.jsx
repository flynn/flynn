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
							"Add Services"
						) : (
							<span className="connect-with-github">
								<i className="icn-github-mark" />
								Connect with Github
							</span>
						)}
				</RouteLink>
			</section>
		);
	}
});

export default AppsListHeader;
