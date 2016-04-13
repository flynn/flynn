import Config from 'dashboard/config';

var ClusterBackup = React.createClass({
	displayName: "Views.ClusterBackup",

	render: function () {
		return (
			<section className="panel-row full-height">
				<section className="panel full-height" style={{
					padding: '1rem'
				}}>
					<header>
						<h1>Cluster backup</h1>
					</header>

					<a className="btn-green" href={Config.endpoints.cluster_controller +'/backup?key='+ Config.user.controller_key}>Create and download</a>
				</section>
			</section>
		);
	}
});

export default ClusterBackup;
