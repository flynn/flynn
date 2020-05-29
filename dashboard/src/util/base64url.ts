export function encode(str: string): string {
	return btoa(str)
		.replace(/\//g, '_')
		.replace(/\+/g, '-')
		.replace(/=+$/, '');
}
