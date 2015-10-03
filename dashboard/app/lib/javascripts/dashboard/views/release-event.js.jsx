import Timestamp from './timestamp';
import PrettyRadio from './pretty-radio';

var ReleaseEvent = React.createClass({
	render: function () {
		var envDiff = this.props.envDiff;
		var release = this.props.release;

		var children = [
			<div key="title" style={{display: 'flex'}}>
				<i className='icn-right' style={{marginRight: '0.5rem'}} />
				<div>
					Release {release.id}
				</div>
			</div>,
			<ul key="diff" style={{
				listStyle: 'none',
				padding: 0,
				margin: 0
			}}>
				{envDiff.map(function (d, i) {
					return (
						<li key={i} style={{
							padding: 0
						}}>
							{d.op === 'replace' || d.op === 'add' ? (
								<small>{d.key}: {d.value.length > 68 ? d.value.slice(0, 65) + '...' : d.value}</small>
							) : (
								<small><del>{d.key}</del></small>
							)}
						</li>
					);
				}, this)}
			</ul>
		];
		if (this.props.timestamp) {
			children.push(
				<div key="timestamp">
					<Timestamp timestamp={this.props.timestamp} />
				</div>
			);
		}
		return (
			<article {...this.props}>
				{this.props.selectable ? (
					<PrettyRadio onChange={this.__handleChange} checked={this.props.selected}>
						<div className="body">
							{children}
						</div>
					</PrettyRadio>
				) : (
					children
				)}
			</article>
		);
	},

	__handleChange: function (e) {
		if (e.target.checked) {
			this.props.onSelect(this.props.event);
		}
	}
});

export default ReleaseEvent;
