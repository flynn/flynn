/** @jsx React.DOM */
//= require ./github-sources
//= require ./github-repos
//= require ./github-repo
//= require ./route-link

(function () {

"use strict";

var RouteLink = FlynnDashboard.Views.RouteLink;

FlynnDashboard.Views.Github = React.createClass({
	displayName: "Views.Github",

	render: function () {
		return (
			<section>
				<header className="page-header">
					<h1>GitHub repos</h1>
					<RouteLink path="/" className="back-link">
						Go back to cluster
					</RouteLink>
				</header>

				<section className="panel">
					<FlynnDashboard.Views.GithubSources
						selectedSource={this.props.selectedSource} />
				</section>

				<section className="panel-row">
					<section className="github-repos-panel">
						<FlynnDashboard.Views.GithubRepos
							selectedSource={this.props.selectedSource}
							selectedType={this.props.selectedType} />
					</section>

					<section className="github-repo-panel">
						{this.props.selectedRepo ? (
							<FlynnDashboard.Views.GithubRepo
								ownerLogin={this.props.selectedRepo.ownerLogin}
								name={this.props.selectedRepo.name}
								selectedPanel={this.props.selectedRepoPanel}
								selectedBranchName={this.props.selectedBranchName} />
						) : (
							<span className="placeholder">Select a repo on the left to get started</span>
						)}
					</section>
				</section>
			</section>
		);
	}
});

})();
