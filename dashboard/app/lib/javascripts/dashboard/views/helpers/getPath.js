(function () {

"use strict";

Dashboard.Views.Helpers.getPath = function (params) {
	var path = Marbles.history.path;
	var pathParams = Marbles.QueryParams.deserializeParams(path.split("?")[1] || "");
	params = Marbles.QueryParams.replaceParams.apply(null, [pathParams].concat(params));
	path = Marbles.history.pathWithParams(path.split("?")[0], params);
	return path;
};

})();
