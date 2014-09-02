/** @jsx React.DOM */

(function () {

"use strict";

FlynnDashboard.Views.EditEnv = React.createClass({
	displayName: "Views.EditEnv",

	render: function () {
		return (
			<ul className="edit-env">
				{this.state.env.map(function (env, i) {
					return (
						<li key={i}>
							<AppEnv
								index={i}
								name={env.key}
								value={env.value}
								onChange={this.handleEnvChange} />
						</li>
					);
				}.bind(this))}
				<li>
					<AppEnv
						key={'new-env'}
						ref="newEnv"
						index={null}
						name={null}
						value={null}
						onChange={this.handleEnvChange} />
				</li>
			</ul>
		);
	},

	getInitialState: function () {
		return {
			env: []
		};
	},

	componentWillMount: function () {
		this.__setEnv(this.props.env || {});
	},

	componentWillReceiveProps: function (props) {
		this.__setEnv(props.env || {});
	},

	componentDidUpdate: function () {
		if (this.__focusNewEnv) {
			delete this.__focusNewEnv;
			this.refs.newEnv.focusNameField();
		}
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

		if (index === null) {
			index = env.length;
			env.push({});
			this.__focusNewEnv = true;
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
		env = __env;

		this.__setEnv(env);
		this.props.onChange(env);
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
	},

	handleValueChange: function () {
		var newValue = this.refs.value.getDOMNode().value;
		this.setState({value: newValue});
	},

	handleNameBlur: function () {
		this.propagateChange();
	},

	handleValueBlur: function () {
		this.propagateChange();
	},

	propagateChange: function () {
		var oldName = this.props.name;
		var newName = this.state.name.trim();
		var oldValue = this.props.value;
		var newValue = this.state.value.trim();

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
