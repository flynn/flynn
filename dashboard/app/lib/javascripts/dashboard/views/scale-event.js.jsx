import Timestamp from './timestamp';
import PrettyRadio from './pretty-radio';

var ScaleEvent = React.createClass({
	render: function () {
		var prevProcesses = this.props.prevProcesses;
		var diff = this.props.diff;
		var timestamp = this.props.timestamp;

		var children = [
			<div key='div' style={{display: 'flex'}}>
				<i className={this.props.delta >= 0 ? 'icn-up' : 'icn-down'} style={{marginRight: '0.5rem'}} />
				<ul style={{
					listStyle: 'none',
					padding: 0,
					margin: 0,
					display: 'flex'
				}}>
					{diff.map(function (d) {
						var delta;
						if (d.op === 'replace') {
							delta = d.value - (prevProcesses[d.key] || 0);
						}
						return (
							<li key={d.key} style={{
								padding: 0,
								marginRight: '1rem'
							}}>
								{d.op === 'add' ? (
									<span>{d.key}: {d.value}</span>
								) : null}
								{d.op === 'replace' ? (
									<span>{d.key}: {d.value} {delta !== 0 ? '('+(delta > 0 ? '+' : '')+delta+')' : null}</span>
								) : null}
								{d.op === 'remove' ? (
									<del>{d.key}</del>
								) : null}
							</li>
						);
					})}
				</ul>
			</div>
		];
		if (timestamp) {
			children.push(
				<div key="timestamp">
					<Timestamp timestamp={timestamp} />
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

export default ScaleEvent;
