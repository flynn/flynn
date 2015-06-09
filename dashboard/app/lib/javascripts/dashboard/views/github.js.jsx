import GithubSources from './github-sources';
import GithubRepos from './github-repos';
import GithubRepo from './github-repo';
import RouteLink from './route-link';

var Github = React.createClass({
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
					<GithubSources
						selectedSource={this.props.selectedSource} />
				</section>

				<section className="panel-row">
					<section className="github-repos-panel">
						<GithubRepos
							selectedSource={this.props.selectedSource}
							selectedType={this.props.selectedType} />
					</section>

					<section className="github-repo-panel">
						{this.props.selectedRepo ? (
							<GithubRepo
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

export default Github;
