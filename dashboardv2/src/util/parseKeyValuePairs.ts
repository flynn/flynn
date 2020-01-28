export default function parseKeyValuePairs(str: string): Iterable<[string, string]> {
	let offset = 0;
	let len = str.length;
	return {
		*[Symbol.iterator]() {
			let key = '';
			let val = '';
			let i = offset;
			while (offset < len) {
				while (str.slice(i++)[0] !== '=') {
					if (i === len) return;
					key = str.slice(offset, i);
				}
				offset = i;
				if (str.slice(i)[0] === '"') {
					i++;
					offset++;
					while (!(str.slice(i++)[0] === '"' && str.slice(i - 2)[0] !== '\\')) {
						if (i === len) return;
						val = str.slice(offset, i);
					}
					val = val.replace(/\\"/g, '"'); // unescape quotes (e.g. JSON)
				} else {
					let keyFound = false;
					while (i++ < len) {
						val = str.slice(offset, i);
						if (val[val.length - 1] === '=' && str[i] !== '=' && str[i] !== '\n') {
							keyFound = true;
							break;
						}
					}
					if (keyFound) {
						// backtrack until beginning of key
						// (let outer loop parse it)
						while (i-- > offset) {
							if (str[i] === '\n') {
								val = str.slice(offset, i);
								i++;
								break;
							}
						}
					}
				}
				offset = i;
				yield [
					key.trim(),
					val.trim().replace(/\\n/g, '\n') // unescape newlines
				] as [string, string];
			}
		}
	};
}
