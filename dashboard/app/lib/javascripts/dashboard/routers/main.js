import Router from 'marbles/router';
import { extend } from 'marbles/utils';
import Config from '../config';
import LoginModel from '../views/models/login';
import LoginComponent from '../views/login';
import InstallCertComponent from '../views/install-cert';
import ClusterBackupComponent from '../views/cluster-backup';
import ClusterStatusComponent from '../views/cluster-status';

var MainRouter = Router.createClass({
	displayName: "routers.main",

	routes: [
		{ path: "", handler: "root" },
		{ path: "login", handler: "login", auth: false },
		{ path: "installcert", handler: "installCert", auth: false },
		{ path: "backup", handler: "clusterBackup" },
		{ path: "status", handler: "clusterStatus" }
	],

	root: function (params) {
		delete params[0].token;
		this.history.navigate("/apps", {
			replace: true,
			params: params
		});
	},

	login: function (params) {
		var redirectPath = params[0].redirect || null;
		if (redirectPath && redirectPath.indexOf("//") !== -1) {
			redirectPath = null;
		}
		if ( !redirectPath ) {
			redirectPath = "";
		}

		var performRedirect = function () {
			this.history.navigate(decodeURIComponent(redirectPath));
		}.bind(this);

		if (Config.authenticated) {
			performRedirect();
			return;
		}

		if (params[0].token) {
			LoginModel.setValue("token", params[0].token);
			LoginModel.performLogin().then(function () {
				performRedirect();
			}).catch(function () {
				var paramsWithoutToken = [extend({}, params[0], { token: null })];
				this.login(paramsWithoutToken);
			}.bind(this));
		} else {
			React.render(React.createElement(
				LoginComponent, {
					onSuccess: performRedirect
				}), this.context.el);
		}
	},

	installCert: function (params) {
		if (window.location.protocol === "https:") {
			this.history.navigate("", {params: params});
			return;
		}
		var handleSubmit = function (e) {
			e.preventDefault();
			this.context.__isCertInstalled().then(function () {
				Config.unfreezeNav();
				this.history.navigate("/login", {params: params});
			}.bind(this));
		}.bind(this);
		React.render(React.createElement("form", { onSubmit: handleSubmit},
			React.createElement("section", { className: "panel" },
				React.createElement(
					InstallCertComponent, {
						certURL: Config.endpoints.cert
					}))), this.context.el);
	},

	clusterBackup: function () {
		React.render(
			React.createElement(ClusterBackupComponent),
			this.context.el);
	},

	clusterStatus: function () {
		React.render(
			React.createElement(ClusterStatusComponent),
			this.context.el);
	}

});

export default MainRouter;
