var ProgressMeter = React.createClass({
	render: function () {
		var percent = this.props.percent;
		var description = this.props.description;
		return (
			<div style={{
				display: 'flex'
			}}>
				<div style={{
					paddingTop: '0.25rem'
				}}>{description}</div>
				<progress style={{
					flexGrow: 1,
					height: '2rem',
					marginLeft: '1rem'
				}} value={percent} max={100} />
			</div>
		);
	}
});

export default ProgressMeter;
