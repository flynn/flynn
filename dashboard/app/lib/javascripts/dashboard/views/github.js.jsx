//= require ./github-sources
//= require ./github-repos
//= require ./github-repo
//= require ./route-link

(function () {

"use strict";

var RouteLink = Dashboard.Views.RouteLink;

Dashboard.Views.Github = React.createClass({
	displayName: "Views.Github",

	render: function () {
		return (
			<section className="github-container">
				<header className="page-header">
					<h1>GitHub repos</h1>
					<RouteLink path={this.props.getClusterPath()} className="back-link">
						{this.props.getGoBackToClusterText()}
					</RouteLink>
				</header>

				<section className="panel">
					<Dashboard.Views.GithubSources
						selectedSource={this.props.selectedSource} />
				</section>

				<section className="panel-row">
					<section className="github-repos-panel">
						<Dashboard.Views.GithubRepos
							selectedSource={this.props.selectedSource}
							selectedType={this.props.selectedType} />
					</section>

					<section className="github-repo-panel">
						{this.props.selectedRepo ? (
							<Dashboard.Views.GithubRepo
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
