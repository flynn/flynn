//= require_self
//= require ./config
//= require ./load_stripe
//= require marbles/utils
//= require_tree ./views

(function (global) {

	function initSponsorBtn(el) {
		var container = document.createElement('span');
		el.parentElement.insertBefore(container, el);
		container.appendChild(el);

		React.renderComponent(
			Flynn.Views.SponsorButton({}),
			container
		);
	}

	global.Flynn = {
		Views: {},

		run: function () {
			this.__initSponsorForm();
			this.__initSponsorBtns();
			this.__initSponsorConfirm();
		},

		formatDollarAmount: function (cents) {
			var _tmp = Math.round(cents / 100).toString().split('.');
			var dollars = _tmp[0],
					cents = _tmp[1];

			if (cents) {
				if (cents.length === 1) {
					cents = cents + '0';
				}

				cents = '.'+ cents;
			} else {
				cents = '';
			}

			var len = dollars.length;
			var groupSep = ',';
			var nGroups = Math.floor((len - 1) / 3);
			_tmp = '';
			var n = 0;
			for (var i = 0; i < len; i++) {
				_tmp = _tmp + dollars[len-1 - i];

				if (nGroups > 0 && (n++) === 2) {
					n = 0;
					nGroups--;
					_tmp = _tmp + groupSep;
				}
			}
			dollars = _tmp.split('').reverse().join('');

			return "$"+ dollars + cents + " USD";
		},

		withStripe: function (callback) {
			function performCallback() {
				callback(window.Stripe);
			}
			if (window.Stripe === undefined) {
				Flynn.StripeLoader.loadStripe(performCallback);
			} else {
				performCallback();
			}
		},

		__initSponsorForm: function () {
			var container = document.createElement('div');
			document.body.appendChild(container);

			this.sponsorForm = React.renderComponent(
				this.Views.SponsorForm({
					onShow: function () {
						window.location.hash = "sponsor";
					},

					onHide: function () {
						window.location.hash = "";
					}
				}),
				container
			);

			this.__checkHashFragment();
			window.addEventListener('popstate', this.__handleChangeLocationState, false);
			window.addEventListener('pushstate', this.__handleChangeLocationState, false);
		},

		__handleChangeLocationState: function (e) {
			Flynn.__checkHashFragment();
		},

		__checkHashFragment: function () {
			if (window.location.hash === "#sponsor") {
				this.sponsorForm.show();
			} else {
				this.sponsorForm.hide();
			}
		},

		__initSponsorBtns: function () {
			var sponsorBtns = document.querySelectorAll('[data-sponsor]');
			for (var i = 0, _len = sponsorBtns.length; i < _len; i++) {
				initSponsorBtn(sponsorBtns[i]);
			}
		},

		__initSponsorConfirm: function () {
			var el = document.getElementById('sponsor-confirm-container');
			if (el) {
				var q = {};

				var _q = window.location.search.slice(1).split('&');
				var key, val, _tmp;
				for (var i = 0, _len = _q.length; i < _len; i++) {
					_tmp = _q[i].split('=');
					key = decodeURIComponent(_tmp[0]);
					val = decodeURIComponent(_tmp[1]);
					q[key] = val;
				}

				React.renderComponent(
					Flynn.Views.SponsorConfirm({
						type: q.type === 'monthly' ? 'monthly' : '',
						amount: parseInt(q.amount)
					}),
					el
				);
			}
		}
	};

})(this);
