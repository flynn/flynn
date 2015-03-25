import Modal from '../modal';
import Dispatcher from '../../dispatcher';
import { default as BtnCSS, red as RedBtnCSS } from '../css/button';

var ClusterDelete = React.createClass({
	render: function () {
		return (
			<Modal visible={true} onHide={this.__handleAbort}>
				<form onSubmit={this.__handleSubmit}>
					<header>
						<h2>Delete cluster {this.props.clusterID}?</h2>
					</header>

					<button type="submit" style={RedBtnCSS}>Delete Cluster</button>
					<button type="text" style={BtnCSS} onClick={function (e) {
						e.preventDefault();
						e.stopPropagation();
						this.__handleAbort();
					}.bind(this)}>Cancel</button>
				</form>
			</Modal>
		);
	},

	__handleAbort: function () {
		Dispatcher.dispatch({
			name: 'CANCEL_CLUSTER_DELETE',
			clusterID: this.props.clusterID
		});
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'CONFIRM_CLUSTER_DELETE',
			clusterID: this.props.clusterID
		});
	}
});

export default ClusterDelete;
