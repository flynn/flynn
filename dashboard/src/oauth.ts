export async function base64(input: ArrayBuffer): Promise<string> {
	let resolve: (str: string) => void;
	let reject: (error: Error) => void;
	const p = new Promise<string>((rs, rj) => {
		resolve = rs;
		reject = rj;
	});

	const blob = new Blob([input], { type: 'application/octet-binary' });
	var reader = new FileReader();
	reader.addEventListener('error', (e) => {
		reader.abort();
		reject(reader.error as Error);
	});
	reader.addEventListener('load', (e) => {
		const res = (reader.result || '') as string;
		// trim data:*/*;base64, prefix
		resolve(res.slice(res.indexOf('base64,') + 7));
	});

	reader.readAsDataURL(blob);

	return p;
}

export async function sha256(input: string): Promise<ArrayBuffer> {
	const encoder = new TextEncoder();
	const data = encoder.encode(input);
	return crypto.subtle.digest('SHA-256', data);
}

export function randomString(length: number): string {
	const randomValues = random(length * 2);
	return hex(randomValues).slice(0, length);
}

export function random(length: number): ArrayBuffer {
	const buffer = new ArrayBuffer(length);
	const array = new Uint32Array(buffer);
	crypto.getRandomValues(array);
	return buffer;
}

function hex(input: ArrayBuffer): string {
	const view = new Int32Array(input);
	return Array.from(view, function(b) {
		return ('0' + (b & 0xff).toString(16)).slice(-2);
	}).join('');
}
