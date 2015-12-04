import { List, ListItem } from './list';
import Dispatcher from '../dispatcher';

var Clusters = React.createClass({
	render: function () {
		var currentClusterID = this.props.state.currentClusterID;
		var clusters = this.props.state.clusters;
		var prompts = this.props.state.prompts;
		return (
			<div>
				<h2>Clusters</h2>

				<List>
					<ListItem selected={currentClusterID === null} path="/" params={[{cloud: this.props.state.currentCloudSlug}]}>New</ListItem>

					{clusters.map(function (cluster) {
						var installState = cluster.state;
						return (
							<ListItem
								key={cluster.attrs.ID}
								selected={currentClusterID === cluster.attrs.ID}
								path={'/clusters/:id'}
								params={[{ id: cluster.attrs.ID }]}
								style={{
									selectors: [
										['> a', {
											display: 'flex'
										}]
									]
								}}>

								<div style={{
									flexGrow: 1,
									WebkitFlexGrow: 1
								}}>{cluster.attrs.name}</div>
								<div>
									{prompts[cluster.attrs.ID] ? (
										<span>
											<span className="fa fa-bell" />
											&nbsp;
										</span>
									) : null}

									{installState.currentStep === 'install' && (installState.deleting || !installState.failed) ? (
										<span>
											<span className="fa fa-cog fa-spin" />
											&nbsp;
										</span>
									) : null}

									{installState.failed ? (
										<span>
											<span className="fa fa-exclamation-triangle" />
											&nbsp;
										</span>
									) : null}

									<span className={"fa "+ (installState.deleting ? "fa-eye-slash" : "fa-trash")} onClick={function (e) {
										e.preventDefault();
										e.stopPropagation();
										this.__handleClusterDeleteBtnClick(cluster);
									}.bind(this)} />
								</div>
							</ListItem>
						);
					}.bind(this))}
				</List>
			</div>
		);
	},

	__handleClusterDeleteBtnClick: function (cluster) {
		if (cluster.state.deleting) {
			Dispatcher.dispatch({
				name: 'CONFIRM_CLUSTER_DELETE',
				clusterID: cluster.attrs.ID
			});
		} else {
			Dispatcher.dispatch({
				name: 'CLUSTER_DELETE',
				clusterID: cluster.attrs.ID
			});
		}
	}
});
export default Clusters;
