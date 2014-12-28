//= require ./route-link

(function () {

"use strict";

Dashboard.Views.AppsListHeader = React.createClass({
	displayName: "Views.AppsListHeader",

	render: function () {
		return (
			<section className="clearfix">
				<Dashboard.Views.RouteLink
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
				</Dashboard.Views.RouteLink>
			</section>
		);
	}
});

})();
