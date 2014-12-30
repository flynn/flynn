//= require ../actions/nav
//= require ./route-link

(function () {

"use strict";

var NavActions = Dashboard.Actions.Nav;

Dashboard.Views.Nav = React.createClass({
	displayName: "Views.Nav",

	render: function () {
		var isAuthenticated = this.props.authenticated;
		var activeItem = this.__getActiveItem();
		var navItems = this.state.items;

		return (
			<ul>
				{navItems.map(function (item) {
					return (
						<li key={item.path} title={item.title} className={item === activeItem ? "active" : ""}>
							<Dashboard.Views.RouteLink path={item.path}>
								<i className={item.icon} />
							</Dashboard.Views.RouteLink>
						</li>
					);
				}.bind(this))}
				<li title={isAuthenticated ? "Log out" : "Log in"} onClick={NavActions.handleAuthBtnClick}>
					<a onClick={function(e){e.preventDefault();}}><i className="icn-auth" /></a>
				</li>
			</ul>
		);
	},

	getInitialState: function () {
		return {
			items: [
				{ title: "Dashboard", icon: "icn-dashboard", path: "/" }
			]
		};
	},

	__getActiveItem: function () {
		var currentPath = "/"+ Marbles.history.getPath();
		var sortedItems = this.state.items.slice(0).sort(function (a, b) {
			return b.path.localeCompare(a.path);
		});
		for (var i = 0, len = sortedItems.length, path; i < len; i++) {
			path = sortedItems[i].path;
			if (currentPath.substr(0, path.length) === path) {
				return sortedItems[i];
			}
		}
		return null;
	}
});

})();
