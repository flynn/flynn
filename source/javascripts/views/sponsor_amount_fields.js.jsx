/** @jsx React.DOM */

Flynn.Views.SponsorAmountFields = React.createClass({
	displayName: "Flynn.Views.SponsorAmountFields",

	getInitialState: function () {
		return {
			valid: null,

			customMonthlyAmount: null,
			customOnetimeAmount: null
		};
	},

	getDefaultProps: function () {
		return {};
	},

	handleValueUpdated: function (newValue) {
		this.props.handleValuesUpdated({
			amount: parseInt(newValue)
		});
	},

	handleCustomMonthlyAmountChange: function () {
		var dollarAmount = this.refs.customMonthlyAmountInput.getDOMNode().value;
		var cents = (parseInt(dollarAmount) * 100);
		if (isNaN(cents)) {
			cents = null;
		}

		this.setState({
			customMonthlyAmount: cents,
			amount: cents
		});

		this.props.handleValuesUpdated({
			amount: cents
		});
	},

	handleCustomMonthlyAmountKeyDown: function (e) {
		if (e.keyCode === 9 && !e.shiftKey) {
			e.preventDefault();
			this.props.focusNextInput();
		}
	},

	selectCustomMonthlyAmount: function () {
		this.refs.customMonthlyAmountRadio.getDOMNode().checked = true;
	},

	focusCustomMonthlyAmountInput: function () {
		this.refs.customMonthlyAmountInput.getDOMNode().focus();
	},

	handleCustomOnetimeAmountChange: function () {
		var dollarAmount = this.refs.customOnetimeAmountInput.getDOMNode().value;
		var cents = (parseInt(dollarAmount) * 100);
		if (isNaN(cents)) {
			cents = null;
		}

		this.setState({
			customOnetimeAmount: cents,
			amount: cents
		});

		this.props.handleValuesUpdated({
			amount: cents
		});
	},

	selectCustomOnetimeAmount: function () {
		this.refs.customOnetimeAmountRadio.getDOMNode().checked = true;
	},

	focusCustomOnetimeAmountInput: function () {
		this.refs.customOnetimeAmountInput.getDOMNode().focus();
	},

	handleMonthlyAmountSelected: function (e) {
		var cents = parseInt(e.target.value);

		this.setState({
			amount: cents
		});

		this.props.handleValuesUpdated({
			contributionType: 'monthly',
			amount: cents
		});

		if (e.target !== this.refs.customMonthlyAmountRadio.getDOMNode()) {
			this.props.focusNextInput();
		}
	},

	handleOnetimeAmountSelected: function (e) {
		var cents = this.state.customOnetimeAmount;

		this.setState({
			amount: cents
		});

		this.props.handleValuesUpdated({
			contributionType: 'onetime',
			amount: cents
		});
	},

	render: function () {
		var InputGroup = Flynn.Views.InputGroup;
		var monthlyAmountInputs = this.props.suggestedMonthlyAmounts.map(function (amount) {
			return (
				<InputGroup key={amount}>
					<label>
						<input
							type="radio"
							name="amount"
							value={amount}
							onChange={this.handleMonthlyAmountSelected}
						/>
						<span>{Flynn.formatDollarAmount(amount)}</span>
					</label>
				</InputGroup>
			);
		}.bind(this));

		var customMonthlyAmount = "";
		if (this.state.customMonthlyAmount !== null) {
			customMonthlyAmount = this.state.customMonthlyAmount / 100;
		}

		var customOnetimeAmount = "";
		if (this.state.customOnetimeAmount !== null) {
			customOnetimeAmount = this.state.customOnetimeAmount / 100;
		}

		return (
			<div>
				<section>
					<header>
						<h5>Monthly contribution</h5>
					</header>

					{monthlyAmountInputs}

					<InputGroup>
						<label onClick={this.focusCustomMonthlyAmountInput}>
							<input
								type="radio"
								name="amount"
								ref="customMonthlyAmountRadio"
								value={this.state.customMonthlyAmount}
								onChange={this.handleMonthlyAmountSelected}
							/>
							<input
								type="text"
								size="6"
								ref="customMonthlyAmountInput"
								onFocus={this.selectCustomMonthlyAmount}
								onChange={this.handleCustomMonthlyAmountChange}
								value={customMonthlyAmount}
								onKeyDown={this.handleCustomMonthlyAmountKeyDown}
							/> USD per month
						</label>
					</InputGroup>

				</section>

				<section>
					<header>
						<h5>One time contribution</h5>
					</header>

					<InputGroup>
						<label onClick={this.focusCustomOnetimeAmountInput}>
							<input
								type="radio"
								name="amount"
								ref="customOnetimeAmountRadio"
								value={this.state.customOnetimeAmount}
								onChange={this.handleOnetimeAmountSelected}
							/>
							<input
								type="text"
								size="6"
								ref="customOnetimeAmountInput"
								onFocus={this.selectCustomOnetimeAmount}
								onChange={this.handleCustomOnetimeAmountChange}
								value={customOnetimeAmount}
							/> USD per month
						</label>
					</InputGroup>
				</section>
			</div>
		);
	}
});
