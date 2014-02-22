/** @jsx React.DOM */

Flynn.Views.SponsorForm = React.createClass({
	displayName: "Flynn.Views.SponsorForm",

	getInitialState: function () {
		return {
			stripeToken: null,
			contributionType: null,
			submitting: false,
			values: {},
			firstStep: true,
			alert: null
		};
	},

	getDefaultProps: function () {
		return {
			suggestedMonthlyAmounts: [
				10000,
				50000,
				100000
			],
			selectedMonthlyAmount: 500000
		};
	},

	handleValuesUpdated: function (values) {
		this.setState({
			values: Marbles.Utils.extend({}, this.state.values, values)
		});
	},

	focusSubmitBtn: function () {
		this.refs.submit.getDOMNode().focus();
	},

	handleSubmit: function (e) {
		e.preventDefault();

		if (this.state.firstStep) {
			this.setState({
				firstStep: false
			});
		} else {
			this.setState({ submitting: true });

			Flynn.withStripe(function (Stripe) {
				Stripe.card.createToken({
					number: this.state.values.ccNumber,
					exp_month: this.state.values.ccMonth,
					exp_year: this.state.values.ccYear,
					cvc: this.state.values.ccCVC
				}, function (status, res) {
					if (res.error) {
						this.setState({
							submitting: false,
							alert: {
								type: 'error',
								text: res.error.message
							}
						});
					} else {
						this.refs.token.getDOMNode().value = res.id;
						this.refs.form.getDOMNode().submit();
					}
				}.bind(this));
			}.bind(this));
		}
	},

	handleBackBtnClick: function (e) {
		e.preventDefault();

		this.setState({
			firstStep: true
		});
	},

	// called from the outside world
	toggleVisibility: function () {
		this.refs.modal.toggleVisibility();
	},

	isSubmitDisabled: function () {
		if (this.state.submitting) {
			return false;
		}

		var firstStepValid = (
			(this.state.values.amount !== null) && this.state.values.contributionType
		);

		if (this.state.firstStep) {
			return !firstStepValid;
		} else {
			return !firstStepValid || !(
				this.state.values.ccNumber && this.state.values.ccMonth && this.state.values.ccYear && this.state.values.ccCVC && this.state.values.email && this.state.values.name
			);
		}
	},

	render: function () {
		var Modal = Flynn.Views.Modal,
				InputGroup = Flynn.Views.InputGroup,
				EmailField = Flynn.Views.EmailField,
				NameField = Flynn.Views.NameField,
				SponsorAmountFields = Flynn.Views.SponsorAmountFields,
				CreditCardFields = Flynn.Views.CreditCardFields;

		var alert;
		if (this.state.alert) {
			alert = (
				<div className={"alert "+ this.state.alert.type}>{this.state.alert.text}</div>
			);
		}

		return (
			<Modal ref="modal">
				<form
					accept-charset="UTF-8"
					action="https://sponsor-flynn.herokuapp.com"
					method="POST"
					ref="form"
					onSubmit={this.handleSubmit}>

					<input type='hidden' ref='token' name='token' value={this.state.stripeToken} />
					<input type='hidden' name='type' value={this.state.values.contributionType} />

					{alert}

					<header>
						<h3>
							<small className={"pull-left sponsor-back-btn"+ (this.state.firstStep ? " hidden" : "")}>
								<a href="#back" onClick={this.handleBackBtnClick}>Back</a>
							</small>
							Sponsor Flynn
						</h3>
					</header>

					<div className={this.state.firstStep ? "" : "hidden"}>
						<SponsorAmountFields
							handleValuesUpdated={this.handleValuesUpdated}
							suggestedMonthlyAmounts={this.props.suggestedMonthlyAmounts}
							focusNextInput={this.focusSubmitBtn}
						/>

						<button
							type="submit"
							ref="submit"
							className="btn btn-primary"
							disabled={this.isSubmitDisabled()}>Contribute now</button>
						<a
							href="mailto:contact@flynn.io?subject=We'd%20like%20to%20sponsor%20Flynn"
							className="btn btn-primary">Contact us</a>
					</div>

					<div className={this.state.firstStep ? "hidden" : ""}>
						<section>
							<InputGroup>
								<EmailField
									label="Email"
									name="email"
									ref="email"
									handleValuesUpdated={this.handleValuesUpdated}
								/>
							</InputGroup>
						</section>

						<section>
							<InputGroup>
								<NameField
									label="Name"
									name="name"
									handleValuesUpdated={this.handleValuesUpdated}
								/>
							</InputGroup>
						</section>

						<CreditCardFields
							initialValues={{}}
							handleValuesUpdated={this.handleValuesUpdated}
						/>

						<button
							type="submit"
							className="btn btn-primary"
							disabled={this.isSubmitDisabled()}>Contribute {Flynn.formatDollarAmount(this.state.values.amount)}{this.state.values.contributionType === 'monthly' ? " Monthly" : ""}</button>
						<a
							href="mailto:contact@flynn.io?subject=We'd%20like%20to%20sponsor%20Flynn"
							className="btn btn-primary">Contact us</a>
					</div>
				</form>
			</Modal>
		);
	}
});
