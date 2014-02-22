/** @jsx React.DOM */

Flynn.Views.CreditCardFields = React.createClass({
	displayName: "Flynn.Views.CreditcardFields",

	getInitialState: function () {
		return {
			ccNumberMsg: null,
			ccNumberValid: null,

			msg: null,
			ccMonthValid: null,
			ccYearValid: null,
			ccCVCValid: null,
			ccMonth: null,
			ccYear: null
		};
	},

	getDefaultProps: function () {
		return {
			validationMonthRegex: /^\d{2}$/,
			validationYearRegex: /^\d{4}$/,
			expCVCInvalidMsg: "Please enter a valid expiry and CVC."
		};
	},

	componentWillReceiveProps: function (props) {
		if (props.initialValues) {
			var initialValues = props.initialValues;
			this.setState({
				ccMonth: initialValues.ccMonth,
				ccYear: initialValues.ccYear
			});
		}
	},

	handleCardNumberUpdated: function (newValue) {
		this.props.handleValuesUpdated({
			ccNumber: newValue
		});
	},

	handleCardMonthUpdated: function (newValue) {
		this.setState({
			ccMonth: newValue
		});
		this.props.handleValuesUpdated({
			ccMonth: newValue
		});

		if (this.state.ccYear) {
			this.performCardExpiryValidation();
		}
	},

	handleCardYearUpdated: function (newValue) {
		this.setState({
			ccYear: newValue
		});
		this.props.handleValuesUpdated({
			ccYear: newValue
		});

		if (this.state.ccMonth) {
			this.performCardExpiryValidation();
		}
	},

	handleCardCVCUpdated: function (newValue) {
		this.props.handleValuesUpdated({
			ccCVC: newValue
		});
	},

	performCardNumberValidation: function (value, callbacks) {
		Flynn.withStripe(function (Stripe) {
			if (Stripe.card.validateCardNumber(value)) {
				this.setState({
					ccNumberValid: true,
					ccNumberMsg: null
				});
				if (callbacks) {
					callbacks.success();
				}
			} else {
				this.setState({
					ccNumberValid: false,
					ccNumberMsg: "Please enter a valid card number."
				});
				if (callbacks) {
					callbacks.failure();
				}
			}
		}.bind(this));
	},

	performCardExpiryValidation: function () {
		Flynn.withStripe(function (Stripe) {
			if (Stripe.card.validateExpiry(this.state.ccMonth, this.state.ccYear)) {
				this.setState({
					ccMonthValid: true,
					ccYearValid: true
				});
				if (this.state.ccCVCValid) {
					this.setState({
						msg: null
					});
				}
			} else {
				this.setState({
					ccMonthValid: false,
					ccYearValid: false,
					msg: this.props.expCVCInvalidMsg
				});
			}
		}.bind(this));
	},

	performCardMonthValidation: function (value, callbacks) {
		if (this.props.validationMonthRegex.test(value) && Number(value) > 0 && Number(value) <= 12) {
			if (this.state.ccYearValid !== false && this.state.ccCVCValid !== false) {
				this.setState({
					msg: null,
					ccMonthValid: true
				});
			} else {
				this.setState({
					ccMonthValid: true
				});
			}
			if (callbacks) {
				callbacks.success();
			}
		} else {
			this.setState({
				msg: this.props.expCVCInvalidMsg,
				ccMonthValid: false
			});
			if (callbacks) {
				callbacks.failure();
			}
		}
	},

	performCardYearValidation: function (value, callbacks) {
		if (this.props.validationYearRegex.test(value) && Number(value) >= (new Date()).getFullYear()) {
			if (this.state.ccMonthValid !== false && this.state.ccCVCValid !== false) {
				this.setState({
					msg: null,
					ccYearValid: true
				});
			} else {
				this.setState({
					ccYearValid: true
				});
			}
			if (callbacks) {
				callbacks.success();
			}
		} else {
			this.setState({
				msg: this.props.expCVCInvalidMsg,
				ccYearValid: false
			});
			if (callbacks) {
				callbacks.failure();
			}
		}
	},

	performCardCVCValidation: function (value, callbacks) {
		Flynn.withStripe(function (Stripe) {
			if (Stripe.card.validateCVC(value)) {
				if (this.state.ccMonthValid !== false && this.state.ccYearValid !== false) {
					this.setState({
						msg: null,
						ccCVCValid: true
					});
				} else {
					this.setState({
						ccCVCValid: true
					});
				}
				if (callbacks) {
					callbacks.success();
				}
			} else {
				this.setState({
					msg: this.props.expCVCInvalidMsg,
					ccCVCValid: false
				});
				if (callbacks) {
					callbacks.failure();
				}
			}
		}.bind(this));
	},

	// called from the outside world
	clearValidation: function () {
		this.setState(this.getInitialState());
	},

	render: function () {
		var InputField = Flynn.Views.InputField,
				InputGroup = Flynn.Views.InputGroup;

		var msg = <div className="info">{String.fromCharCode(160)}</div>;
		if (this.state.msg) {
			msg = <div className="info">{this.state.msg}</div>;
		}

		return (
			<div>
				<InputGroup label="Credit card number">
					<InputField
						type="text"
						valid={this.state.ccNumberValid}
						msg={this.state.ccNumberMsg}
						performValidation={this.performCardNumberValidation}
						handleValueUpdated={this.handleCardNumberUpdated}
						initialValue={this.props.initialValues.ccNumber}
					/>
				</InputGroup>

				<InputGroup label="Credit card info">
					<InputField
						type="text"
						size={3}
						maxLength={2}
						label={null}
						placeholder="MM"
						valid={this.state.ccMonthValid}
						performValidation={this.performCardMonthValidation}
						handleValueUpdated={this.handleCardMonthUpdated}
						initialValues={this.props.initialValues.ccMonth}
					/>

					<InputField
						type="text"
						size={4}
						maxLength={4}
						label={null}
						placeholder="YYYY"
						valid={this.state.ccYearValid}
						performValidation={this.performCardYearValidation}
						handleValueUpdated={this.handleCardYearUpdated}
						initialValues={this.props.initialValues.ccYear}
					/>

					<InputField
						type="text"
						size={3}
						label={null}
						placeholder="CVC"
						valid={this.state.ccCVCValid}
						performValidation={this.performCardCVCValidation}
						handleValueUpdated={this.handleCardCVCUpdated}
						initialValues={this.props.initialValues.ccCVC}
					/>

					{msg}
				</InputGroup>
			</div>
		);
	}
});
