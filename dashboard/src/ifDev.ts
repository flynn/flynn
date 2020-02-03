export default function ifDev<T>(fn: () => T | undefined) {
	if (process.env.NODE_ENV !== 'production') {
		return fn();
	}
	return undefined;
}
