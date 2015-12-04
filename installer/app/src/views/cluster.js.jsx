import Dashboard from './dashboard';

var Cluster = React.createClass({
	render: function () {
		var state = this.props.state;
		var clusterID = this.props.clusterID;
		return (
			<div>
				<Dashboard state={state} clusterID={clusterID} />
			</div>
		);
	}
});
export default Cluster;
