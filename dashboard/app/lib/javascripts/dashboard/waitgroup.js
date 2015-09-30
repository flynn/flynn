var WaitGroup = function () {
	this.n = 0;
	this.promise = Promise.resolve();
	this.__resolve = function(){};
	this.resolved = true;
};

WaitGroup.prototype.start = function () {
	this.promise = new Promise(function (rs) {
		this.__resolve = rs;
	}.bind(this));
	this.resolved = false;
};

WaitGroup.prototype.addOne = function () {
	if (this.resolved) {
		this.start();
	}
	this.n++;
};

WaitGroup.prototype.removeOne = function () {
	if (this.resolved) {
		throw new Error('WaitGroup: Can\'t remove from resolved group');
	}
	this.n--;
	if (this.n === 0) {
		this.resolve();
	}
};

WaitGroup.prototype.then = function (fn) {
	return this.promise.then(fn);
};

WaitGroup.prototype.resolve = function () {
	this.n = 0;
	this.resolved = true;
	this.__resolve();
};

export default WaitGroup;
