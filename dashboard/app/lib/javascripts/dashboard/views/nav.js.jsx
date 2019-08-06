import NavActions from '../actions/nav';
import RouteLink from './route-link';
import Config from '../config';

var Nav = React.createClass({
	displayName: 'Views.Nav',

	render: function() {
		var isAuthenticated = this.props.authenticated;
		var activeItem = this.__getActiveItem();
		var navItems = this.state.items;

		return (
			<ul>
				{navItems.map(
					function(item) {
						return (
							<li key={item.path} title={item.title} className={item === activeItem ? 'active' : ''}>
								<RouteLink path={item.path}>
									<i className={item.icon} />
								</RouteLink>
							</li>
						);
					}.bind(this)
				)}
				<li title={isAuthenticated ? 'Log out' : 'Log in'} onClick={NavActions.handleAuthBtnClick}>
					<a
						onClick={function(e) {
							e.preventDefault();
						}}
						href="/"
					>
						<i className="icn-auth" />
					</a>
				</li>
			</ul>
		);
	},

	getInitialState: function() {
		return {
			items: [
				{ title: 'Dashboard', icon: 'icn-dashboard', path: '/' },
				{ title: 'Providers', icon: 'icn-databases', path: '/providers' },
				{ title: 'Cluster backup', icon: 'icn-download', path: '/backup' },
				{ title: 'Cluster status', icon: 'icn-system-status', path: '/status' }
			]
		};
	},

	__getActiveItem: function() {
		var currentPath = '/' + Config.history.getPath();
		var sortedItems = this.state.items.slice(0).sort(function(a, b) {
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

export default Nav;
