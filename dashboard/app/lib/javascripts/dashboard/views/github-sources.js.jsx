import GithubUserStore from '../stores/github-user';
import GithubOrgsStore from '../stores/github-orgs';
import getPath from './helpers/getPath';
import RouteLink from './route-link';

var orgsStoreId = "default";
var userStoreId = "default";

function getState() {
	var state = {};

	state.orgs = GithubOrgsStore.getState(orgsStoreId).orgs;
	state.user = GithubUserStore.getState(userStoreId).user;

	return state;
}

var Source = React.createClass({
	displayName: "Views.GithubSources - Source",

	render: function () {
		var source = this.props.source;
		return (
			<RouteLink path={this.props.path}>
				<img src={source.avatarURL + "&size=50"} title={source.name ? source.name +" ("+ source.login +")" : source.login} />
			</RouteLink>
		);
	}

});

var GithubSources = React.createClass({
	displayName: "Views.GithubSources",

	render: function () {
		return (
			<ul className="github-sources">
				{this.state.user ? (
					<li className={this.props.selectedSource === null ? "selected" : null}>
						<Source path={getPath([{ org: null, type: null }])} source={this.state.user} />
					</li>
				) : null}
				{this.state.orgs.map(function (org) {
					return (
						<li key={org.id} className={this.props.selectedSource === org.login ? "selected" : null}>
							<Source path={getPath([{ org: org.login, type: null }])} source={org} />
						</li>
					);
				}, this)}
			</ul>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		GithubUserStore.addChangeListener(userStoreId, this.__handleStoreChange);
		GithubOrgsStore.addChangeListener(orgsStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		this.setState(getState(props));
	},

	componentWillUnmount: function () {
		GithubUserStore.removeChangeListener(userStoreId, this.__handleStoreChange);
		GithubOrgsStore.removeChangeListener(orgsStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(getState(this.props));
	}
});

export default GithubSources;
