import QueryParams from 'marbles/query_params';
import { pathWithParams } from 'marbles/history';
import Config from '../../config';

var getPath = function (params) {
	var path = Config.history.path;
	var pathParams = QueryParams.deserializeParams(path.split("?")[1] || "");
	params = QueryParams.replaceParams.apply(null, [pathParams].concat(params));
	path = pathWithParams(path.split("?")[0], params);
	return path;
};

export default getPath;
