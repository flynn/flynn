package assetmatrix

var scssJS = `
var sass = require('node-sass');
var readline = require('readline');
var fs = require('fs');
var filepath = require('path');

var rl = readline.createInterface({
	input: process.stdin,
	output: process.stdout,
	terminal: false
});

var getData = function(callback) {
	rl.question('<data>\n', function (tmpFileName) {
		callback(fs.readFileSync(tmpFileName, { encoding: 'utf8' }));
	});
};

var assetRoot = null;
var getAssetRoot = function (callback) {
	if (assetRoot !== null) {
		callback(assetRoot);
		return;
	}
	rl.question('<assetRoot>\n', function (path) {
		assetRoot = path.replace(/^\.{1,2}\//, '').replace(/\/?$/, '/');
		callback(assetRoot);
	});
};

var getAssetPath = function(path, callback) {
	rl.question('<assetPath>:'+ path +'\n', callback);
};

var getAssetOutputPath = function(path, callback) {
	rl.question('<assetOutputPath>:'+ path +'\n', callback);
};

getData(function (data) {
	if (data.split('{').length !== data.split('}').length) {
		console.error(data);
		process.exit(1);
	}
	sass.render({
		data: data,
		importer: function (url, prev, done) {
			if (prev !== 'stdin' && url[0] === '.') {
				getAssetRoot(function (root) {
					url = filepath.join(filepath.dirname(prev), url);
					if (url.substr(0, root.length) === root) {
						url = url.substr(root.length);
					}
					getAssetPath(url, function (path) {
						done({ file: path });
					});
				});
				return;
			}
			getAssetPath(url, function (path) {
				done({ file: path });
			});
		},
		functions: {
			'asset-url($url)': function (url, done) {
				getAssetOutputPath(url.getValue(), function (path) {
					done(new sass.types.String('url("'+ path +'")'));
				});
			},
			'asset-data-url($url)': function (url, done) {
				getAssetOutputPath(url.getValue(), function (path) {
					done(new sass.types.String('url("'+ path +'")'));
				});
				// TODO: get contents of file and base64 it
			}
		}
	}, function (err, result) {
		rl.close();
		if (err) {
			console.error(err);
			process.exit(1);
		}
		console.log('<output>\n');
		console.log(result.css.toString());
	});
});
`
