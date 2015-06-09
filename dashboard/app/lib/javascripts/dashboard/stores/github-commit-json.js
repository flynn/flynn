var rewriteGithubCommitJSON = function (commitJSON) {
	var committer = commitJSON.committer || commitJSON.commit.committer;
	var author = commitJSON.author || commitJSON.commit.author;
	return {
		committer: {
			avatarURL: committer.avatar_url,
			name: commitJSON.commit.committer.name
		},
		author: {
			avatarURL: author.avatar_url,
			name: commitJSON.commit.author.name
		},
		committedAt: Date.parse(commitJSON.commit.committer.date),
		createdAt: Date.parse(commitJSON.commit.author.date),
		sha: commitJSON.sha,
		message: commitJSON.commit.message,
		githubURL: commitJSON.html_url
	};
};
export { rewriteGithubCommitJSON };
