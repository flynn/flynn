import UserAgent from './css/user-agent';
import Panel from './panel';
import { List, ListItem } from './list';
import { extend } from 'marbles/utils';
import Dispatcher from '../dispatcher';

var Main = React.createClass({
	getDefaultProps: function () {
		return {
			css: {
				margin: 16,
				display: UserAgent.isSafari() ? '-webkit-flex' : 'flex'
			},
			childrenCSS: {
				flexGrow: 1,
				WebkitFlexGrow: 1
			}
		};
	},

	render: function () {
		var currentClusterID = this.state.currentClusterID;
		var clusters = this.state.clusters;
		return (
			<div style={this.props.css}>
				<div style={extend({}, this.props.childrenCSS, { marginRight: 16, maxWidth: 360, minWidth: 300 })}>
					<Panel style={{ height: '100%' }}>
						<h2>Clusters</h2>

						<List>
							<ListItem selected={currentClusterID === null} path="/">New</ListItem>

							{clusters.map(function (cluster) {
								var installState = cluster.getInstallState();
								return (
									<ListItem
										key={cluster.ID}
										selected={currentClusterID === cluster.ID}
										path={'/clusters/:id'}
										params={[{ id: cluster.ID }]}
										innerStyle={{
											display: UserAgent.isSafari() ? '-webkit-flex' : 'flex'
										}}>

										<div style={{
											flexGrow: 1,
											WebkitFlexGrow: 1
										}}>{cluster.name}</div>
										<div>
											{installState.prompt !== null ? (
												<span>
													<span className="fa fa-bell" />
													&nbsp;
												</span>
											) : null}

											{installState.currentStep === 'install' && !installState.failed ? (
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
					</Panel>
				</div>

				<div style={this.props.childrenCSS}>
					{this.props.children}
				</div>
			</div>
		);
	},

	componentDidMount: function () {
		this.props.dataStore.addChangeListener(this.__handleDataChange);
	},

	componentWillUnmount: function () {
		this.props.dataStore.removeChangeListener(this.__handleDataChange);
	},

	getInitialState: function () {
		return this.__getState();
	},

	__getState: function () {
		return this.props.dataStore.state;
	},

	__handleDataChange: function () {
		this.setState(this.__getState());
	},

	__handleClusterDeleteBtnClick: function (cluster) {
		if (cluster.getInstallState().deleting) {
			Dispatcher.dispatch({
				name: 'CONFIRM_CLUSTER_DELETE',
				clusterID: cluster.ID
			});
		} else {
			Dispatcher.dispatch({
				name: 'CLUSTER_DELETE',
				clusterID: cluster.ID
			});
		}
	}
});
export default Main;
