export default function arrayToFormattedString(arr: Array<string>): string {
	if (arr.length === 1) return arr[0];
	return `${arr.slice(0, arr.length - 1).join(', ')}, and ${arr[arr.length - 1]}`;
}
