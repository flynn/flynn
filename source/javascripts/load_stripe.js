Flynn.StripeLoader = {
	__callbacks: [],
	__loading: false,

	loadStripe: function (callback) {
		var script;

		this.__callbacks.push(callback);

		if (this.__loading === false) {
			this.__loading = true;

			script = document.createElement('script');
			script.src = Flynn.config.stripeJSURL;
			script.onload = this.handleStripeLoaded.bind(this);
			document.body.appendChild(script);
		}
	},

	handleStripeLoaded: function () {
		Stripe.setPublishableKey(Flynn.config.STRIPE_KEY);

		function throwAsync (e) {
			setTimeout(function () {
				throw e;
			}, 0);
		}
		for (var i = 0, _ref = this.__callbacks, _len = _ref.length; i < _len; i++) {
			try {
				this.__callbacks[i]();
			} catch (e) {
				throwAsync(e);
			}
		}
		this.__callbacks = [];
	}
};
