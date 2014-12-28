//= require ./external-link
//= require ./timestamp

(function () {

"use strict";

var ExternalLink = Dashboard.Views.ExternalLink;
var Timestamp = Dashboard.Views.Timestamp;

Dashboard.Views.GithubPull = React.createClass({
	displayName: "Views.GithubPull",

	render: function () {
		var pull = this.props.pull;

		var userAvatarURL = pull.user.avatarURL;
		var userAvatarURLParams;
		var userAvatarURLParts;
		if (userAvatarURL) {
			userAvatarURLParts = userAvatarURL.split("?");
			userAvatarURLParams = Marbles.QueryParams.deserializeParams(userAvatarURLParts[1] || "");
			userAvatarURLParams = Marbles.QueryParams.replaceParams(userAvatarURLParams, {
				size: 50
			});
			userAvatarURL = userAvatarURLParts[0] + Marbles.QueryParams.serializeParams(userAvatarURLParams);
		}

		return (
			<article className="github-pull">
				<img className="avatar" src={userAvatarURL} />
				<div className="body">
					<div className="message">
						<ExternalLink href={pull.githubUrl}>
							{pull.title} #{pull.number}
						</ExternalLink>
					</div>
					<div>
						<span className="name">
							{pull.user.login}
						</span>
						<span className="timestamp">
							<ExternalLink href={pull.url}>
								<Timestamp timestamp={pull.createdAt} />
								{pull.updatedAt !== pull.createdAt ? (
									<span>
										&nbsp;(Updated <Timestamp timestamp={pull.updatedAt} />)
									</span>
								) : null}
							</ExternalLink>
						</span>
					</div>
				</div>
				{this.props.children}
			</article>
		);
	}
});

})();
