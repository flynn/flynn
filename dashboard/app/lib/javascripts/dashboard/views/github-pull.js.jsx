import QueryParams from 'marbles/query_params';
import Timestamp from './timestamp';
import ExternalLink from './external-link';

var GithubPull = React.createClass({
	displayName: "Views.GithubPull",

	render: function () {
		var pull = this.props.pull;

		var userAvatarURL = pull.user.avatarURL;
		var userAvatarURLParams;
		var userAvatarURLParts;
		if (userAvatarURL) {
			userAvatarURLParts = userAvatarURL.split("?");
			userAvatarURLParams = QueryParams.deserializeParams(userAvatarURLParts[1] || "");
			userAvatarURLParams = QueryParams.replaceParams(userAvatarURLParams, {
				size: 50
			});
			userAvatarURL = userAvatarURLParts[0] + QueryParams.serializeParams(userAvatarURLParams);
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

export default GithubPull;
