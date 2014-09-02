/** @jsx React.DOM */
//= require ./route-link
//= require ./external-link

(function () {

"use strict";

var RouteLink = FlynnDashboard.Views.RouteLink;
var ExternalLink = FlynnDashboard.Views.ExternalLink;

FlynnDashboard.Views.GithubAuth = React.createClass({
	displayName: "Views.GithubAuth",

	render: function () {
		return (
			<section>
				<header>
					<h1>Connect with Github</h1>
					<RouteLink path="/" className="back-link">
						Go back to cluster
					</RouteLink>
				</header>

				<section className="panel github-auth">
					<ol>
						<li>
							<ExternalLink href={"https://github.com/settings/tokens/new"+ Marbles.QueryParams.serializeParams([{
									scopes: "repo,read:org,read:public_key",
									description: "Flynn Dashboard"
								}])} className="btn-green connect-with-github">
								<i className="icn-github-mark" />
								Generate Token
							</ExternalLink>
						</li>

						<li>
							Set the <code>GITHUB_TOKEN</code> environment variable to the generated token for the dashboard API server.
						</li>

						<li>
							Restart the dashbaord API server.
						</li>

						<li>
							<a href="">Reload this page</a>.
						</li>
					</ol>
				</section>
			</section>
		);
	}
});

})();
