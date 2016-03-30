var UserAgent = {
	isFirefox: function () {
		return window.navigator.userAgent.match(/\bFirefox\b/) !== null;
	},

	isSafari: function () {
		return window.navigator.userAgent.match(/\bSafari\b/) !== null;
	},

	isChrome: function () {
		return window.navigator.userAgent.match(/\bChrome\b/) !== null;
	},

	isOSX: function () {
		return window.navigator.userAgent.match(/\bOS X\b/) !== null;
	},

	isWindows: function () {
		return window.navigator.userAgent.match(/\bWindows\b/) !== null;
	},

	isLinux: function () {
		return window.navigator.userAgent.match(/\bLinux\b/) !== null;
	}
};
export default UserAgent;
