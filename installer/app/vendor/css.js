var idCounter = 0;
var sheetCounter = 0;
var genID = function () {
	return 's' + (idCounter++);
};

var DOMRenderer = {
	remove: function (id) {
		var el = document.getElementById(id);
		if (el) {
			el.parentElement.removeChild(el);
		}
	},

	insert: function (id, refNodeId, cssStr) {
		var styleEl = document.createElement('style');
		styleEl.id = id;
		styleEl.innerHTML = cssStr;

		var refNode = document.getElementById(refNodeId);
		refNode = refNode ? refNode : document.head.firstChild;
		if (refNode) {
			document.head.insertBefore(styleEl, refNode);
		} else {
			document.head.appendChild(styleEl);
		}
	}
};

var cssPropertyName = function (k) {
	return k.replace(/([A-Z])/g, function (m) {
		return '-'+ m.toLowerCase();
	});
};

var CSS = function (options) {
	options = options || {};
	this.elements = [];
	this.id = null;
	this.renderer = options.renderer || DOMRenderer;
	this.transformers = options.transformers || [];
};

CSS.prototype.removeFromDOM = function () {
	var styleEl = document.getElementById(this.id);
	if ( !styleEl ) {
		return;
	}
	document.head.removeChild(styleEl);
};

CSS.prototype.commit = function () {
	var oldId = this.id;
	this.id = 's'+ (sheetCounter++) +''+ Date.now();
	this.renderer.insert(this.id, oldId, this.toCSSString());
	this.renderer.remove(oldId);
};

CSS.prototype.toCSSString = function () {
	return this.elements.map(function (element) {
		var selector = '#'+ element.id;
		return this.moduleToCSSString(selector, element.compiled);
	}.bind(this)).join('\n');
};

CSS.prototype.moduleToCSSString = function (selector, module) {
	var self = this;
	var cssPropertyStr = function (k, v) {
		self.transformers.forEach(function (t) {
			var res = t(k, v);
			k = res[0];
			v = res[1];
		});
		return cssPropertyName(k) +': '+ v +';';
	};

	var compileProperties = function (s, m) {
		var res = '';
		res += s +' {\n';
		var keys = Object.keys(m).sort().filter(function (k) {
			return k !== 'selectors';
		})
		if (keys.length === 0) {
			return '';
		}
		res += keys.map(function (k) {
			return '\t'+ cssPropertyStr(k, m[k]);
		}).join('\n');
		res += '\n}';
		return res;
	};

	var joinSelectors = function (a, b) {
		var sep ='';
		if (b.substr(0,1) !== ':') {
			sep = ' ';
		}
		return a + sep + b;
	};

	var flattenSelectors = function (s, m) {
		var selectors = [];
		if (m.hasOwnProperty('selectors')) {
			m.selectors.forEach(function (item) {
				selectors.push([joinSelectors(s, item[0]), item[1]]);
				selectors.push.apply(selectors, flattenSelectors(joinSelectors(s, item[0]), item[1]));
			});
		}
		return selectors;
	};

	var str = compileProperties(selector, module);

	var selectors = flattenSelectors(selector, module);
	selectors.forEach(function (item) {
		var s = item[0];
		var m = item[1];
		if (str !== '') {
			str += '\n';
		}
		str += compileProperties(s, m);
	});

	return str;
};

var CSSElement = function (modules) {
	this.id = genID();
	this.modules = modules;
	this.compiled = {};
};

CSSElement.prototype.commit = function () {
	this.compile();
	this.stylesheet.commit();
};

var mergeModules = function (objA, objB) {
	var diff = CSS.diff(objB, objA).filter(function (d) {
		return d.op !== 'remove';
	});
	return CSS.applyDiff(diff, objA);
};

CSSElement.prototype.compile = function () {
	var compiled = {};
	this.modules.forEach(function (m) {
		compiled = mergeModules(compiled, m);
	});
	this.compiled = compiled;
}

CSS.prototype.createElement = function () {
	var element = new CSSElement(Array.prototype.slice.call(arguments));
	element.stylesheet = this;
	this.elements.push(element);
	return element;
};

var isEqual = function (a, b) {
	if (typeof a !== typeof b) {
		return false;
	}
	if (Array.isArray(a)) {
		if (!Array.isArray(b)) {
			return false;
		}
		if (a.length !== b.length) {
			return false;
		}
		for (var i = 0, len = a.length; i < len; i++) {
			if ( !isEqual(a[i], b[i]) ) {
				return false;
			}
		}
		return true;
	}
	if (typeof a === 'object') {
		for (var k in a) {
			if ( !a.hasOwnProperty(k) ) {
				continue;
			}
			if ( !b.hasOwnProperty(k) ) {
				return false;
			}
			if ( !isEqual(a[k], b[k]) ) {
				return false;
			}
		}
		return true;
	}
	return a === b;
};

var marshalJSONPointer = function (parts) {
	return parts.map(function (p) {
		if (p === '~') {
			return '~0';
		}
		if (p === '/') {
			return '~1';
		}
		return p;
	}).join('/');
};

var unmarshalJSONPointer = function (pointer) {
	var parts = pointer.split('/');
	return parts.map(function (p) {
		if (p === '~0') {
			return '~';
		}
		if (p === '~1') {
			return '/';
		}
		return p;
	});
};

CSS.diff = function (objA, objB, path) {
	path = path || [''];
	var diff = [];
	for (var k in objA) {
		if ( !objA.hasOwnProperty(k) ) {
			continue;
		}
		if (objB.hasOwnProperty(k)) {
			if ( !isEqual(objB[k], objA[k]) ) {
				if (typeof objB[k] === 'object' && typeof objA[k] === 'object') {
					diff = diff.concat(CSS.diff(objA[k], objB[k], path.concat([k])));
				} else {
					diff.push({op: "replace", path: marshalJSONPointer(path.concat([k])), value: objA[k]});
				}
			}
		} else {
			diff.push({op: "add", path: marshalJSONPointer(path.concat([k])), value: objA[k]});
		}
	}
	for (k in objB) {
		if ( !objB.hasOwnProperty(k) ) {
			continue;
		}
		if ( !objA.hasOwnProperty(k) ) {
			diff.push({op: "remove", path: marshalJSONPointer(path.concat([k]))});
		}
	}
	return diff;
};

var deepClone = function (obj) {
	if (typeof obj === 'object' && obj !== null) {
		if (Array.isArray(obj)) {
			return obj.map(function (item) {
				return deepClone(item);
			});
		} else {
			var newObj = {};
			for (var k in obj) {
				if ( !obj.hasOwnProperty(k) ) {
					continue;
				}
				newObj[k] = deepClone(obj[k]);
			}
			return newObj;
		}
	} else {
		return obj;
	}
};

CSS.applyDiff = function (diff, obj) {
	var newObj = deepClone(obj);
	diff.forEach(function (item) {
		var path = unmarshalJSONPointer(item.path);
		path.shift(); // ignore leading /
		var lastKey = path.pop();
		var ctx;
		var getLastParent = function () {
			var ref = newObj;
			var k;
			while (k = path.shift()) {
				if (Array.isArray(ref)) {
					k = parseInt(k, 10);
				}
				ref = ref[k];
			}
			return ref;
		};
		switch (item.op) {
			case 'replace':
				ctx = getLastParent();
				if (Array.isArray(ctx)) {
					lastKey = parseInt(lastKey, 10);
				}
				ctx[lastKey] = item.value;
			break;

			case 'add':
				var secondLastKey = path.pop();
				ctx = getLastParent();
				if (Array.isArray(ctx[secondLastKey])) {
					lastKey = parseInt(lastKey, 10);
					ctx[secondLastKey] = ctx[secondLastKey].slice(0, lastKey).concat([item.value]).concat(ctx[secondLastKey].slice(lastKey));
				} else {
					if (secondLastKey) {
						ctx = ctx[secondLastKey];
					}
					ctx[lastKey] = item.value;
				}
			break;

			case 'remove':
				var secondLastKey = path.pop();
				ctx = getLastParent();
				if (Array.isArray(ctx[secondLastKey])) {
					lastKey = parseInt(lastKey, 10);
					ctx[secondLastKey] = ctx[secondLastKey].slice(0, lastKey).concat(ctx[secondLastKey].slice(lastKey+1));
				} else {
					if (secondLastKey) {
						ctx = ctx[secondLastKey];
					}
					delete ctx[lastKey];
				}
			break;
		}
	});
	return newObj;
};

export { CSSElement };
export default CSS;
