import RouteLink from './route-link';
import ExternalLink from './external-link';
import Config from 'dashboard/config';

var AppsListHeader = React.createClass({
	displayName: 'Views.AppsListHeader',

	render: function() {
		return (
			<section>
				<section className="clearfix">
					<RouteLink className="btn-green float-right" path="/github">
						{this.props.githubAuthed ? (
							'Create application'
						) : (
							<span className="connect-with-github">
								<i className="icn-github-mark" />
								Connect with Github
							</span>
						)}
					</RouteLink>

					<RouteLink className="btn-green float-right" path="/providers" style={{ marginRight: '1rem' }}>
						Provision database
					</RouteLink>
				</section>

				<section className="clearfix" style={{ marginTop: '1rem' }}>
					<ExternalLink className="btn-green float-right" href={'https://dashboardv2.' + Config.default_route_domain}>
						Dashboard v2
					</ExternalLink>
				</section>
			</section>
		);
	}
});

export default AppsListHeader;
