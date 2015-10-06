import { extend } from 'marbles/utils';

var objectDiff = function (oldEnv, newEnv) {
	var diff = [];
	for (var k in newEnv) {
		if ( !newEnv.hasOwnProperty(k) ) {
			continue;
		}
		if (oldEnv.hasOwnProperty(k)) {
			if (oldEnv[k] !== newEnv[k]) {
				diff.push({op: "replace", key: k, value: newEnv[k]});
			}
		} else {
			diff.push({op: "add", key: k, value: newEnv[k]});
		}
	}
	for (k in oldEnv) {
		if ( !oldEnv.hasOwnProperty(k) ) {
			continue;
		}
		if ( !newEnv.hasOwnProperty(k) ) {
			diff.push({op: "remove", key: k});
		}
	}
	return diff;
};

var applyObjectDiff = function (diff, env) {
	var newEnv = extend({}, env);
	diff.forEach(function (item) {
		switch (item.op) {
		case "replace":
			newEnv[item.key] = item.value;
			break;

		case "add":
			newEnv[item.key] = item.value;
			break;

		case "remove":
			if (newEnv.hasOwnProperty(item.key)) {
				delete newEnv[item.key];
			}
			break;
		}
	});
	return newEnv;
};

export { objectDiff, applyObjectDiff };
