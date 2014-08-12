/** @jsx React.DOM */

Flynn.Views.SponsorConfirm = React.createClass({
	displayName: "Flynn.Views.SponsorConfirm",

	render: function () {
		return (
			<div>
        <p>Thanks for your {this.props.type} contribution of {Flynn.formatDollarAmount(this.props.amount)}!</p>
        <br />

        <aside>
					<header>
						<h3>Keep in touch</h3>
					</header>

					<ul className="list-block">
						<li>
							<a href="https://github.com/flynn/flynn">
								GitHub
							</a>
						</li>

						<li>
							<a href="irc://irc.freenode.net/flynn">
								IRC
							</a>
						</li>

						<li>
							<a href="mailto:contact@flynn.io">
								Email
							</a>
						</li>
					</ul>
				</aside>
			</div>
		);
	}
});
