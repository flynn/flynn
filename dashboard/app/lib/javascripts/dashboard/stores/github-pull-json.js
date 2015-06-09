var rewriteGithubPullJSON = function (pullJSON) {
	var stripHTML = function (str) {
		var tmp = document.createElement("div");
		tmp.innerHTML = str;
		return tmp.textContent || tmp.innerText;
	};
	return {
		id: pullJSON.id,
		number: pullJSON.number,
		title: pullJSON.title,
		body: stripHTML(pullJSON.body),
		url: pullJSON.html_url,
		createdAt: pullJSON.created_at,
		updatedAt: pullJSON.updated_at,
		user: {
			login: pullJSON.user.login,
			avatarURL: pullJSON.user.avatar_url
		},
		head: {
			label: pullJSON.head.label,
			ref: pullJSON.head.ref,
			sha: pullJSON.head.sha,
			name: pullJSON.head.repo.name,
			ownerLogin: pullJSON.head.repo.owner.login,
			fullName: pullJSON.head.repo.full_name
		},
		base: {
			label: pullJSON.base.label,
			ref: pullJSON.base.ref,
			sha: pullJSON.base.sha,
			name: pullJSON.base.repo.name,
			ownerLogin: pullJSON.base.repo.owner.login,
			fullName: pullJSON.base.repo.full_name
		}
	};
};
export { rewriteGithubPullJSON };
