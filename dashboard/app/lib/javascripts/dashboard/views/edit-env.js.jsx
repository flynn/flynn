(function () {

"use strict";

Dashboard.Views.EditEnv = React.createClass({
	displayName: "Views.EditEnv",

	render: function () {
		var nRemoved = this.state.nRemoved;
		return (
			<ul className="edit-env">
				{this.state.env.concat([{ isNew: true }]).map(function (env, i) {
					return (
						<li key={nRemoved + i}>
							<AppEnv
								index={env.isNew ? null : i}
								name={env.key}
								value={env.value}
								onChange={this.handleEnvChange} />
						</li>
					);
				}.bind(this))}
			</ul>
		);
	},

	getInitialState: function () {
		return {
			env: [],
			nRemoved: 0
		};
	},

	componentWillMount: function () {
		this.__setEnv(this.props.env || {});
	},

	componentWillReceiveProps: function (props) {
		this.__setEnv(props.env || {});
	},

	__setEnv: function (env) {
		var __env = [];
		for (var k in env) {
			if (env.hasOwnProperty(k)) {
				__env.push({
					key: k,
					value: env[k]
				});
			}
		}
		this.setState({env: __env});
	},

	handleEnvChange: function (index, oldName, newName, newValue) {
		var env = [].concat(this.state.env);
		var nRemoved = this.state.nRemoved;

		if (index === null) {
			index = env.length;
			env.push({});
		}

		if ( !newName ) {
			env = env.slice(0, index).concat(env.slice(index+1, env.length));
		} else {
			env[index] = {
				key: newName,
				value: newValue
			};

			// ensure no duplicate keys
			if (oldName !== newName) {
				var __env = [];
				for (var i = 0, len = env.length; i < len; i++) {
					if (i !== index && env[i].key === newName) {
						nRemoved++;
					} else {
						__env.push(env[i]);
					}
				}
				env = __env;
			}
		}

		__env = {};
		env.forEach(function (i) {
			__env[i.key] = i.value;
		});

		this.setState({ env: env, nRemoved: nRemoved });
		this.props.onChange(__env);
	}
});

var AppEnv = React.createClass({
	displayName: "Views.EditEnv AppEnv",

	render: function () {
		return (
			<div>
				<input
					className="name"
					type='text'
					ref='name'
					value={this.state.name}
					placeholder="ENV key"
					onChange={this.handleNameChange}
					onBlur={this.handleNameBlur} />:

					<input
						type='text'
						ref='value'
						value={this.state.value}
						placeholder="ENV value"
						onChange={this.handleValueChange}
						onBlur={this.handleValueBlur} />
			</div>
		);
	},

	getInitialState: function () {
		return {};
	},

	componentWillMount: function () {
		this.setState({
			name: this.props.name || "",
			value: this.props.value || ""
		});
	},

	componentWillReceiveProps: function (props) {
		this.setState({
			name: props.name || "",
			value: props.value || ""
		});
	},

	handleNameChange: function () {
		var newName = this.refs.name.getDOMNode().value;
		this.setState({name: newName});
		if (this.state.value) {
			this.propagateChange(newName, this.state.value);
		}
	},

	handleValueChange: function () {
		var newValue = this.refs.value.getDOMNode().value;
		this.setState({value: newValue});
		if (this.state.name) {
			this.propagateChange(this.state.name, newValue);
		}
	},

	handleNameBlur: function () {
		this.propagateChange(this.state.name, this.state.value);
	},

	handleValueBlur: function () {
		this.propagateChange(this.state.name, this.state.value);
	},

	propagateChange: function (newName, newValue) {
		var oldName = this.props.name;
		var oldValue = this.props.value;

		if ( !oldName && (!newName || !newValue) ) {
			return;
		}

		if (oldName !== newName || oldValue !== newValue) {
			this.props.onChange(this.props.index, oldName, newName, newValue);
		}
	},

	focusNameField: function () {
		this.refs.name.getDOMNode().focus();
	}
});

})();
