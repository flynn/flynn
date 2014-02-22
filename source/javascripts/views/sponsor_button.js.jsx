/** @jsx React.DOM */

Flynn.Views.SponsorButton = React.createClass({
	displayName: "Flynn.Views.SponsorButton",

	handleClick: function (e) {
		e.preventDefault();
		Flynn.sponsorForm.toggleVisibility();
	},

	render: function () {
		return (
			<a href="#sponsor" className="btn fill" onClick={this.handleClick}>Sponsor</a>
		);
	}
});
