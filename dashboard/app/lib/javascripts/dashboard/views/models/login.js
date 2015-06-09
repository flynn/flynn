import { createClass } from 'marbles/utils';
import State from 'marbles/state';
import Config from '../../config';

function parseKeypath(keypath) {
	var parts = keypath.split(".");
	var keys = [];
	for (var i = 0, p, len = parts.length; i < len; i++) {
		p = parts[i];
		if (p.substr(p.length-1) === "\\") {
			parts[i+1] = p.substr(0, p.length-1) + "." + parts[i+1];
		} else {
			keys.push(p);
		}
	}
	return keys;
}

var Login = createClass({
	displayName: "Models.Login",

	mixins: [State, {
		ctor: {
			validationRequiredKeypaths: ["token"],

			validation: {
				"token": function (token) {
					return new Promise(function (resolve, reject) {
						if (token.length > 0) {
							resolve();
						} else {
							reject("Login token can not be blank");
						}
					});
				}
			}
		}
	}],

	willInitialize: function (attrs) {
		this.state = {
			attrs: attrs,
			validation: {}
		};
		this.__changeListeners = [];
		this.__persisting = false;
	},

	getValue: function (keypath) {
		var keys = parseKeypath(keypath);
		var lastKey = keys.pop();
		var ref = this.state.attrs;
		var k, i, _len;
		for (i = 0, _len = keys.length; i < _len; i++) {
			if (!ref) {
				break;
			}
			k = keys[i];
			ref = ref[k];
		}
		if (!ref || !ref.hasOwnProperty(lastKey)) {
			return;
		}
		return ref[lastKey];
	},

	setValue: function (keypath, value) {
		var keys = parseKeypath(keypath);
		var lastKey = keys.pop();
		var ref = this.state.attrs;
		var k, i, _len;
		for (i = 0, _len = keys.length; i < _len; i++) {
			k = keys[i];
			ref[k] = ref[k] || {};
			ref = ref[k];
		}
		ref[lastKey] = value;

		this.setState({
			attrs: this.state.attrs
		});

		var validation = this.constructor.validation;
		var __validation = this.state.validation;
		if (validation.hasOwnProperty(keypath)) {
			validation[keypath](value).then(function (valid, msg) {
				__validation[keypath] = {
					valid: valid !== undefined ? valid : true,
					msg: msg || null
				};
				this.setState({
					validation: __validation
				});
			}.bind(this), function (msg) {
				if (msg instanceof Error) {
					throw msg;
				}
				__validation[keypath] = {
					valid: false,
					msg: msg || null
				};
				this.setState({
					validation: __validation
				});
			}.bind(this));
		}
	},

	getValidation: function (keypath) {
		return this.state.validation[keypath] || {
			valid: null,
			msg: null
		};
	},

	getValueLink: function (keypath) {
		return {
			value: this.getValue(keypath),
			validation: this.getValidation(keypath),
			requestChange: this.setValue.bind(this, keypath)
		};
	},

	isValid: function () {
		var requiredKeypaths = this.constructor.validationRequiredKeypaths || [];
		var valid = true;
		for (var i = 0, len = requiredKeypaths.length; i < len; i++) {
			if (this.getValidation(requiredKeypaths[i]).valid !== true) {
				valid = false;
				break;
			}
		}
		return valid;
	},

	isPersisting: function () {
		return this.__persisting;
	},

	performLogin: function () {
		this.__persisting = true;
		var attrs = this.state.attrs;
		return Config.client.login(attrs.token).then(function () {
			this.__persisting = false;
			this.setState({});
		}.bind(this), function (err) {
			this.__persisting = false;
			var validation = this.state.validation;
			if (Array.isArray(err)) {
				var res = err[0];
				if (res.field) {
					validation[res.field] = {
						valid:  false,
						msg: res.message
					};
				}
			}
			this.setState({ validation: validation });
			return Promise.reject(err);
		}.bind(this));
	}
});

Login.instances = {};
[
	"getValueLink",
	"getValue",
	"getValidation",
	"setValue",
	"isValid",
	"performLogin",
	"isPersisting",
	"addChangeListener",
	"removeChangeListener"
].forEach(function (methodName) {
	Login[methodName] = function () {
		var instance = Login.instances["default"] || new Login({});
		Login.instances["default"] = instance;
		return instance[methodName].apply(instance, arguments);
	};
});

export default Login;
