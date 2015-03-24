var env = require('system').env;
var config = {
	url: env.URL,
	loginToken: env.LOGIN_TOKEN,
	githubToken: env.GITHUB_TOKEN,
	defaultWaitTimeout: 1000, // 1 second
	maxAppNameLength: 30
};

var values = {};

var extend = function (obj) {
	var others = Array.prototype.slice.call(arguments, 1);
	others.forEach(function (other) {
		for (var k in other) {
			if (other.hasOwnProperty(k)) {
				obj[k] = other[k];
			}
		}
	});
	return obj;
};

var Suite = function (test) {
	this.test = test;

	var proto = this.constructor.prototype;
	for (var k in proto) {
		if (proto.hasOwnProperty(k) && typeof this[k] === 'function') {
			this[k] = this[k].bind(this);
		}
	}
};

extend(Suite.prototype, {
	__casperWaitFn: function (fn, m, callback, timeout) {
		return fn.call(casper, m, callback, callback, timeout || config.defaultWaitTimeout);
	},

	__waitCallbackTimeout: function (callback, timeout) {
		if (typeof callback !== 'function') {
			if (timeout === undefined) {
				timeout = callback;
			}
			callback = function(){};
		}
		return [callback, timeout];
	},

	waitForUrl: function (matcher, desc, callback, timeout) {
		var self = this;
		var res = self.__waitCallbackTimeout(callback, timeout);
		callback = res[0];
		timeout = res[1];
		return this.__casperWaitFn(casper.waitForUrl, matcher, function () {
			self.test.assert(!!casper.getCurrentUrl().match(matcher), desc || 'Page url matches '+ matcher);
		}, timeout).then(callback);
	},

	waitForSelector: function (selector, desc, callback, timeout) {
		var self = this;
		var res = self.__waitCallbackTimeout(callback, timeout);
		callback = res[0];
		timeout = res[1];
		return this.__casperWaitFn(casper.waitForSelector, selector, function () {
			self.test.assertExists(selector, desc || 'Selector exists: '+ selector);
		}, timeout).then(callback);
	},

	waitWhileSelector: function (selector, callback, timeout) {
		var res = this.__waitCallbackTimeout(callback, timeout);
		callback = res[0];
		timeout = res[1];
		return this.__casperWaitFn(casper.waitWhileSelector, selector, callback, timeout);
	},

	Login: function () {
		var self = this;
		self.test.assert(!!casper.page.url.match(/\/login/), 'Login page loaded');
		casper.fillXPath('form', {
			'//input[@type="password"]': config.loginToken
		}, true);
		return self.waitForUrl(/\/apps/, 'Login successful');
	},

	GithubAuth: function () {
		var self = this;
		var githubBtnSelector = '.btn-green[href="/github"]';
		self.test.assertExists(githubBtnSelector, 'Github button exists');
		self.test.assertEqual(casper.fetchText(githubBtnSelector), 'Connect with Github', 'Github button reads "Connect with Github"');
		casper.click(githubBtnSelector);
		return self.waitForUrl(/\/github\/auth/, '', function () {
			var generateTokenBtnSelector = 'a[href^="https://github.com/settings/tokens/new"]';
			self.test.assertExists(generateTokenBtnSelector, 'Generate token button exists');
		}).then(function () {
			casper.fillXPath('form', {
				'//input[@type="text"]': config.githubToken
			}, true);
			return self.waitForUrl(/\/github$/, 'Github auth successful', 20000);
		});
	},

	NavigateGithubStarred: function () {
		var starredLinkSelector = 'a[href$="github?type=star"]';
		this.test.assertExists(starredLinkSelector, 'Starred link exists');
		casper.click(starredLinkSelector);
	},

	LaunchExampleRepo: function () {
		var self = this;
		var exampleRepoLinkSelector = 'a[href*="flynn-examples"]';
		var launchCommitBtnSelector = '.launch-btn';
		var launchBtnSelector = '#secondary .launch-btn';
		return self.waitForSelector(exampleRepoLinkSelector, 'Example repo link exists', 10000).then(function () {
			casper.click(exampleRepoLinkSelector);
			return self.waitForSelector(launchCommitBtnSelector, 'Launch button exists');
		}).then(function () {
			casper.click(launchCommitBtnSelector);
			return self.waitForSelector(launchBtnSelector, '', 10000);
		}).then(function () {
			var nameInputSelector = '#secondary .name+input[type=text]';
			var postgresCheckboxSelector = '#secondary .name+input[type=checkbox]';
			var newEnvKeyInputSelector = '#secondary .edit-env input';
			var newEnvValueInputSelector = '#secondary .edit-env input+span+input';
			self.test.assertExists(launchBtnSelector, 'Launch button exists (modal)');
			self.test.assertExists(nameInputSelector, 'Name input exists');
			self.test.assertExists(postgresCheckboxSelector, 'Postgres checkbox exists');
			self.test.assertExists(newEnvKeyInputSelector, 'Env key input exists');
			self.test.assertExists(newEnvValueInputSelector, 'Env value input exists');
			var fillValues = {};
			fillValues[nameInputSelector] = values.exampleAppName = ('example-app-'+ Date.now()).substr(0, values.maxAppNameLength);
			casper.fillSelectors('body', fillValues);
			values.testEnvKey = ('TEST_'+ Date.now());
			casper.click(newEnvKeyInputSelector);
			casper.sendKeys(newEnvKeyInputSelector, values.testEnvKey);
			values.testEnvValue = ''+ Date.now();
			casper.click(newEnvValueInputSelector);
			casper.sendKeys(newEnvValueInputSelector, values.testEnvValue);
			casper.click(postgresCheckboxSelector);
			self.test.assertExists(postgresCheckboxSelector +':checked', 'Postgres checkbox checked');
			casper.click(launchBtnSelector);
			return self.waitWhileSelector(launchBtnSelector+ '[disabled]', 60000 * 5);
		}).then(function () {
			self.test.assert(casper.fetchText(launchBtnSelector) === 'Continue', 'Example app launched');
			casper.click(launchBtnSelector); // navigate to app
		});
	},

	ValidateLaunchedApp: function () {
		var self = this;
		return self.waitForUrl(/\/apps\/[^\/]+$/, '', function () {
			var editEnvLinkSelector = 'a[href$="/env"]';
			self.test.assertExists(editEnvLinkSelector, 'Edit env link exists');
			casper.click(editEnvLinkSelector);
			return self.waitForSelector('#secondary .edit-env input', '');
		}).then(function () {
			var testEnvKeySelector = 'input[value="'+ values.testEnvKey +'"]';
			var testEnvValueSelector = testEnvKeySelector +'+span+input[value="'+ values.testEnvValue +'"]';
			self.test.assertExists(testEnvKeySelector, 'Env key persisted');
			self.test.assertExists(testEnvValueSelector, 'Env value persisted');
		});
	},

	RemoveGithubToken: function () {
		var self = this;
		self.test.assert(!!casper.page.url.match(/\/apps\/dashboard\/env$/), 'Dashboard edit env page loaded');
		return self.waitForSelector('#secondary input[type=text]', '', function () {
			self.test.assertExists('#secondary input[value=GITHUB_TOKEN]', 'GITHUB_TOKEN env is set');
			casper.fillSelectors('#secondary', {
				'input[value=GITHUB_TOKEN]': ''
			});
			casper.click('#secondary .edit-env+button');
			self.waitForUrl(/\/apps\/dashboard$/, 'Env saved', 10000);
		});
	}
});

casper.test.begin('Dashboard integration test', 27, function (test) {
	var suite = new Suite(test);

	casper.start(config.url);

	casper.then(suite.Login);
	casper.then(suite.GithubAuth);
	casper.then(suite.NavigateGithubStarred);
	casper.then(suite.LaunchExampleRepo);
	casper.then(suite.ValidateLaunchedApp);

	casper.thenOpen(config.url +'/apps/dashboard/env');
	casper.then(suite.RemoveGithubToken);

	casper.run(function () {
		test.done();
	});
});
