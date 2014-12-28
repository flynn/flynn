//= require ./external-link
//= require ./timestamp
//= require ./helpers/findScrollParent

(function () {

"use strict";

var ExternalLink = Dashboard.Views.ExternalLink;
var Timestamp = Dashboard.Views.Timestamp;
var findScrollParent = Dashboard.Views.Helpers.findScrollParent;

Dashboard.Views.GithubCommit = React.createClass({
	displayName: "Views.GithubCommit",

	scrollIntoView: function () {
		var el = this.getDOMNode();
		var scrollParent = findScrollParent(el);
		var offsetTop = el.offsetTop;
		var offsetHeight = el.offsetHeight;
		var scrollParentStyle = window.getComputedStyle(scrollParent);
		var scrollParentHeight = parseInt(scrollParentStyle.height, 10);
		var scrollTop = offsetTop - ((scrollParentHeight - offsetHeight) / 2);
		scrollParent.scrollTop = scrollTop;
	},

	render: function () {
		var commit = this.props.commit;

		var selectable = this.props.selectable;
		var selected = this.props.selected;

		var authorAvatarURL = commit.author.avatarURL;
		var authorAvatarURLParams;
		var authorAvatarURLParts;
		if (authorAvatarURL) {
			authorAvatarURLParts = authorAvatarURL.split("?");
			authorAvatarURLParams = Marbles.QueryParams.deserializeParams(authorAvatarURLParts[1] || "");
			authorAvatarURLParams = Marbles.QueryParams.replaceParams(authorAvatarURLParams, {
				size: 50
			});
			authorAvatarURL = authorAvatarURLParts[0] + Marbles.QueryParams.serializeParams(authorAvatarURLParams);
		}

		return (
			<article className="github-commit">
				<label className={selectable ? "pretty-radio" : "inner"}>
					{selectable ? (
						<input type="radio" name="selected-sha" checked={selected} onChange={this.__handleChange} />
					) : null}
					{selectable ? (
						<div className={"dot"} />
					) : null}

					<img className="avatar" src={authorAvatarURL} />
					<div className="body">
						<div className="message">
							{commit.message.split("\n")[0]}
						</div>
						<div>
							<span className="name">
								{commit.author.name}
							</span>
							<span className="timestamp">
								<ExternalLink href={commit.githubURL}>
									<Timestamp timestamp={commit.createdAt} />
								</ExternalLink>
							</span>
						</div>
					</div>
					{this.props.children}
				</label>
			</article>
		);
	},

	__handleChange: function (e) {
		if (e.target.checked) {
			this.props.onSelect(this.props.commit);
		}
	}
});

})();
