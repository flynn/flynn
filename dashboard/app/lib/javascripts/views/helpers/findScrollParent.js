(function () {
"use strict";

FlynnDashboard.Views.Helpers.findScrollParent = function (el) {
	var ref = el;
	while (ref) {
		switch (window.getComputedStyle(ref).overflow) {
			case "auto":
				return ref;
			case "scroll":
				return ref;
		}
		ref = ref.parentElement;
	}
	return window;
};

})();
