import Dispatcher from '../dispatcher';
import Config from '../config';
import ExternalLink from './external-link';

var ReleaseChannelSelector = React.createClass({
	render: function () {
		var clusterState = this.props.state;
		var releaseChannels = Config.release_channels || [];
		var selectedReleaseChannel = (clusterState.releaseChannel ? (releaseChannels.find(function (ch) {
			return ch.name === clusterState.releaseChannel;
		})) : null) || null;
		var selectedReleaseVersion = clusterState.releaseVersion;

		var ec2Versions = Config.ec2_versions;
		if (clusterState.selectedCloud === 'aws') {
			selectedReleaseChannel = {
				name: 'stable',
				version: (ec2Versions[0] || {}).version || null,
				history: ec2Versions.map(function (v) {
					return { version: v.version };
				})
			};
			releaseChannels = [selectedReleaseChannel];
		}

		return (
			<div style={this.props.style}>
				<label>
					<span>Select version to install:&nbsp;&nbsp;</span>
					<select value={clusterState.releaseChannel} onChange={this.__handleReleaseChannelChange}>
						{releaseChannels.map(function (c) {
							return (
								<option key={c.name} value={c.name}>{c.name}</option>
							);
						}.bind(this))}
					</select>
					{selectedReleaseChannel !== null ? (
						<select value={selectedReleaseVersion || selectedReleaseChannel.version} onChange={function (e) {
							this.__handleReleaseVersionChange(selectedReleaseChannel, e.target.value);
						}.bind(this)}>
							{selectedReleaseChannel.history.map(function (h) {
								return (
									<option key={h.version} value={h.version}>{h.version}</option>
								);
							}.bind(this))}
						</select>
					) : null}
					{selectedReleaseVersion ? (
						<span>
							&nbsp;&nbsp;
							<ExternalLink href={'https://releases.flynn.io/'+ selectedReleaseChannel.name +'/'+ clusterState.releaseVersion}>More info</ExternalLink>
						</span>
					) : null}
					{clusterState.selectedCloud === 'aws' ? (
						<p>EC2 images are pre-built and include a subset of all available releases. You'll have to use the <ExternalLink href="https://flynn.io/docs/installation/manual">manual install process</ExternalLink> or a different cloud provider if you want to use a version which isn't listed above.</p>
					) : null}
				</label>
			</div>
		);
	},

	__handleReleaseChannelChange: function (e) {
		var name = e.target.value;
		var channel = (Config.release_channels || []).find(function (ch) {
			return ch.name === name;
		});
		Dispatcher.dispatch({
			clusterID: 'new',
			name: 'SELECT_RELEASE_CHANNEL',
			releaseChannel: channel.name,
			releaseVersion: channel.version
		});
	},

	__handleReleaseVersionChange: function (channel, version) {
		Dispatcher.dispatch({
			clusterID: 'new',
			name: 'SELECT_RELEASE_VERSION',
			releaseChannel: channel.name,
			releaseVersion: version
		});
	}
});

export default ReleaseChannelSelector;
