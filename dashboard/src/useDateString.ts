import * as React from 'react';

import { MINUTE, isToday, isYesterday } from './dateCommon';

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
