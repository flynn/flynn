var ServiceUnavailable = React.createClass({
	displayName: "Views.ServiceUnavailable",

	render: function () {
		return (
			<section>
				<header>
					<h1>
						Service Unavailable
						{this.props.status > 0 ? (
							<small>{this.props.status}</small>
						) : null}
					</h1>
				</header>

				<p>
					Sorry for the inconvenience. Please <a href="">try again</a> in a few minutes.
				</p>
			</section>
		);
	}
});

export default ServiceUnavailable;
