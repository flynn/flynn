import useDateString from './useDateString';
export default function useRelativeTimeString(dateTime: Date) {
	const dateString = useDateString(dateTime);
	return dateString;
}
