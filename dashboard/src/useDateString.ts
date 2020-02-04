import * as React from 'react';
import roundedDate from './util/roundedDate';

const SECOND = 1000;
const MINUTE = SECOND * 60;
const HOUR = MINUTE * 60;
const DAY = HOUR * 24;

function isToday(d: Date): boolean {
	const TODAY = roundedDate(new Date());
	if (d.getFullYear() !== TODAY.getFullYear()) {
		return false;
	}
	if (d.getMonth() !== TODAY.getMonth()) {
		return false;
	}
	if (d.getDate() !== TODAY.getDate()) {
		return false;
	}
	return true;
}

function isYesterday(d: Date): boolean {
	const YESTERDAY = roundedDate(new Date(Date.now() - DAY));
	if (d.getFullYear() !== YESTERDAY.getFullYear()) {
		return false;
	}
	if (d.getMonth() !== YESTERDAY.getMonth()) {
		return false;
	}
	if (d.getDate() !== YESTERDAY.getDate()) {
		return false;
	}
	return true;
}

function dateToString(date: Date): string {
	if (isToday(date)) {
		return 'Today';
	}
	if (isYesterday(date)) {
		return 'Yesterday';
	}
	return date.toDateString();
}

export default function useDateString(date: Date) {
	const [dateString, setDateString] = React.useState(dateToString(date));
	const maybeSetDateString = React.useCallback(() => {
		const nextDateString = dateToString(date);
		if (dateString === nextDateString) return;
		setDateString(nextDateString);
	}, [date, dateString]);
	React.useEffect(() => {
		const ref = setInterval(() => {
			maybeSetDateString();
		}, 1 * MINUTE);
		return () => {
			clearInterval(ref);
		};
	}, [maybeSetDateString]);
	return dateString;
}
