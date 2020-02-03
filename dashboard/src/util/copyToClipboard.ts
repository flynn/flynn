export default function(text: string) {
	const tmpInput = document.createElement('textarea');
	tmpInput.value = text;
	tmpInput.style.position = 'absolute';
	document.body.appendChild(tmpInput);
	tmpInput.focus();
	tmpInput.setSelectionRange(0, text.length);
	document.execCommand('copy');
	document.body.removeChild(tmpInput);
}
