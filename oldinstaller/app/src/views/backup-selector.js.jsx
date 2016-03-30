import Dispatcher from '../dispatcher';
import FileInput from './file-input';

var BackupSelector = React.createClass({
	render: function () {
		return (
			<div style={this.props.style}>
				<label style={{
					display: 'block',
					maxWidth: 400
				}}>
					<span>Restore from backup:</span>
					<FileInput onChange={this.__handleFileChange} errorMsg={this.state.errorMsg} style={{
						maxWidth: 400
					}} />
				</label>
			</div>
		);
	},

	getInitialState: function () {
		return {
			errorMsg: null
		};
	},

	__handleFileChange: function (file) {
		this.setState({
			errorMsg: null
		});
		Dispatcher.dispatch({
			clusterID: 'new',
			name: 'SELECT_BACKUP',
			file: file
		});
	}
});

export default BackupSelector;
