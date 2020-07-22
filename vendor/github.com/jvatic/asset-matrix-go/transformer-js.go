package assetmatrix

var transformerJS = `
var recast = require('recast');
var Promise = require('es6-promise').Promise;

function runTransformer(inputData) {
	var b = recast.types.builders;
	var moduleName = process.env.MODULE_NAME;
	var modulesGlobalVarName = process.env.MODULES_GLOBAL_VAR_NAME;
	var modulesLocalVarName = process.env.MODULES_LOCAL_VAR_NAME;
	var importMapping = JSON.parse(process.env.IMPORT_MAPPING);
	try {
		var ast = recast.parse(inputData);
	} catch (e) {
		console.error(e);
		console.error(inputData);
		return Promise.resolve({
			body: inputData
		});
	}
	var imports = {};

	var prevPromise = Promise.resolve();
	return Promise.all(ast.program.body.map(function (node, index) {
		switch (node.type) {
			case "ImportDeclaration":
				imports[node.source.value] = {
					specifiers: node.specifiers
				};
				var moduleLookupName = node.source.value;
				return prevPromise = prevPromise.then(function () {
					return new Promise(function (resolve) {
						resolve((function (moduleName) {
							var thisModule = b.memberExpression(b.identifier(modulesLocalVarName), b.literal(moduleName), true);
							ast.program.body[index] = b.variableDeclaration("var", node.specifiers.map(function (sp) {
									var name;
									if (sp.name) {
										name = sp.name.name;
									} else {
										name = sp.id.name;
									}
									return b.variableDeclarator(b.identifier(name), b.logicalExpression(
										"||",
										b.memberExpression(thisModule, b.identifier(sp.id.name), false),
										b.memberExpression(thisModule, b.identifier("default"), false)
									));
								}));
						})(importMapping[moduleLookupName]));
					});
				});

			case "ExportDeclaration":
				var name;
				if (node.default) {
					name = "default";
				}
				if (!node.declaration) {
					// TODO: Find a better way of combining expressions
					ast.program.body[index] = b.ifStatement(b.literal(true), b.blockStatement(node.specifiers.map(function (specifier) {
						var name = specifier.name ? specifier.name.name : specifier.id.name;
						return b.expressionStatement(b.assignmentExpression(
								"=", b.identifier(modulesLocalVarName+'["'+ moduleName +'"].'+ name),
								b.identifier(specifier.id.name)
							));
					})));
					return Promise.resolve(node);
				}
				var expression;
				switch (node.declaration.type) {
					case "FunctionDeclaration":
						var fn = node.declaration;
						if (!name) {
							name = fn.id.name;
						}
						expression = b.functionExpression(null, fn.params, fn.body);
					break;

					case "Identifier":
						if (!name) {
							name = node.declaration.name;
						}
						expression = b.identifier(node.declaration.name);
					break;

					default:
						if (node.declaration.type.match(/Expression$/)) {
							if (!name) {
								name = node.declaration.id.name;
							}
							expression = node.declaration;
						} else {
							console.error('Unsupported export declaration: ', node);
							return Promise.reject();
						}
				}
				ast.program.body[index] = b.expressionStatement(b.assignmentExpression(
						"=", b.identifier(modulesLocalVarName+'["'+ moduleName +'"].'+ name),
						expression
					));
			break;
		}
		return Promise.resolve(node);
	})).then(function () {

		var _body = [b.expressionStatement(b.assignmentExpression(
			"=", b.identifier(modulesLocalVarName+'["'+ moduleName +'"]'),
			b.objectExpression([])
		))].concat(ast.program.body);
		ast.program.body = [
			b.expressionStatement(
				b.callExpression(
					b.functionExpression(
						null, // Anonymize the function expression.
						[b.identifier(modulesLocalVarName)],
						b.blockStatement(
							[b.expressionStatement(b.literal("use strict"))].concat(_body)
						)
					), [b.identifier(modulesGlobalVarName +' = '+ modulesGlobalVarName +' || {}')]))
		];
		return Promise.resolve({
			body: recast.print(ast).code
		});
	}).catch(function (err) {
		console.error(err);
		return Promise.resolve({
			body: inputData
		});
	});
}

var inputData = "";
process.stdin.setEncoding('utf8');
process.stdin.on('readable', function () {
	var chunk = process.stdin.read();
	if (chunk !== null) {
		inputData += chunk;
	}
});
process.stdin.on('end', function () {
	runTransformer(inputData).then(function (event) {
		process.stdout.write(event.body);
		process.exit(0);
	});
});
`
