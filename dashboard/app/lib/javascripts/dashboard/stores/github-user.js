import Store from '../store';
import Config from '../config';

var GithubUser = Store.createClass({
	displayName: "Stores.GithubUser",

	getState: function () {
		return this.state;
	},

	getInitialState: function () {
		return {
			user: null
		};
	},

	didBecomeActive: function () {
		this.__fetchUser();
	},

	__fetchUser: function () {
		Config.githubClient.getUser().then(function (args) {
			var res = args[0];
			this.setState({
				user: this.__rewriteJSON(res)
			});
		}.bind(this));
	},

	__rewriteJSON: function (userJSON) {
		return {
			avatarURL: userJSON.avatar_url,
			login: userJSON.login,
			name: userJSON.name
		};
	}

});

export default GithubUser;
