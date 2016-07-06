import ClusterStatusStore from 'dashboard/stores/cluster-status';

var clusterStatusStoreID = null;

var StatusComponent = function (props) {
	return (
		<article>
			<header>
				<h3>{props.name} <i className={'icn-'+ (props.healthy ? 'pulse' : 'heart')} title={props.healthy ? 'healthy' : 'unhealthy'} /></h3>
			</header>
		</article>
	);
};

var ClusterStatus = React.createClass({
	displayName: 'Views.ClusterStatus',

	render: function () {
		var lines = [];
		if (this.state.lastFetchedAt !== null) {
			lines.push([{
				name: 'cluster ('+ this.state.version +')',
				healthy: this.state.healthy
			}]);
		}

		var services = this.state.services;
		var perLine = 4;
		for (var i = 0, n = 4, len = services.length; i < len; i++) {
			if (n == perLine && len-1 > i+1) {
				n = 1;
				lines.push([]);
			} else {
				n++;
			}
			lines[lines.length-1].push(services[i]);
		}

		return (
			<section className="cluster-status full-height">
				<section className="full-height">
					{lines.map(function (services) {
						return (
							<ul key={services[0].name}>
								{services.map(function (service) {
									return (
										<li key={service.name} className={service.healthy ? 'healthy' : 'unhealthy'}>
											{StatusComponent(service)}
										</li>
									);
								})}
							</ul>
						);
					})}
				</section>
			</section>
		);
	},

	__getState: function () {
		return ClusterStatusStore.getState(clusterStatusStoreID);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		ClusterStatusStore.addChangeListener(clusterStatusStoreID, this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		ClusterStatusStore.removeChangeListener(clusterStatusStoreID, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props));
	}
});

export default ClusterStatus;
