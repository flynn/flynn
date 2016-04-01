import Config from '../config';
var rewriteGithubRepoJSON = function (repoJSON) {
	var cloneURL = repoJSON.clone_url;
	if (repoJSON.private || Config.github_clone_auth_required) {
		cloneURL = cloneURL.replace(/^https?:\/\//, function (m) {
			return m + Config.githubClient.accessToken + "@";
		});
	}
	return {
		id: repoJSON.id,
		name: repoJSON.name,
		language: repoJSON.language,
		description: repoJSON.description,
		ownerLogin: repoJSON.owner.login,
		defaultBranch: repoJSON.default_branch,
		cloneURL: cloneURL
	};
};
export { rewriteGithubRepoJSON };
