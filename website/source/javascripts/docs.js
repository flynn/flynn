(function () {
"use strict";

function bindExample(node) {
	var toggleNode = node.querySelector('.example-toggle');
	var innerNodes = [node.querySelector('.example-request'), node.querySelector('.example-response')];
	var expanded = false;
	var collapsedText = toggleNode.innerText || toggleNode.textContent;
	var expandedText = toggleNode.getAttribute('data-expanded');

	function expand() {
		expanded = true;
		toggleNode.innerText = expandedText;
		toggleNode.textContent = expandedText;
		for (var i = 0, len = innerNodes.length; i < len; i++) {
			innerNodes[i].style.display = 'block';
		}
	}

	function collapse() {
		expanded = false;
		toggleNode.innerText = collapsedText;
		toggleNode.textContent = collapsedText;
		for (var i = 0, len = innerNodes.length; i < len; i++) {
			innerNodes[i].style.display = 'none';
		}
	}

	toggleNode.addEventListener('click', function (e) {
		e.preventDefault();

		if (expanded) {
			collapse();
		} else {
			expand();
		}
	});
}

var exampleNodes = document.querySelectorAll('.example');
for (var i = 0, len = exampleNodes.length; i < len; i++) {
	bindExample(exampleNodes[i]);
}

})();
