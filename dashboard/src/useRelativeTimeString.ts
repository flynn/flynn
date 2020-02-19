import * as React from 'react';
import 'intl-relative-time-format';
import 'intl-relative-time-format/locale-data/en-US';

import { SECOND, MINUTE, HOUR, DAY, WEEK, MONTH, YEAR } from './dateCommon';

export function relativeTimeString(date: Date): string {
	const rtf = new Intl.RelativeTimeFormat('en-US');
	const nowMs = Date.now();
	const dateMs = date.getTime();
	const secondsAgo = Math.round((nowMs - dateMs) / SECOND);
	const minutesAgo = Math.round((nowMs - dateMs) / MINUTE);
	const hoursAgo = Math.round((nowMs - dateMs) / HOUR);
	const daysAgo = Math.round((nowMs - dateMs) / DAY);
	const weeksAgo = Math.round((nowMs - dateMs) / WEEK);
	const monthsAgo = Math.round((nowMs - dateMs) / MONTH);
	const yearsAgo = Math.round((nowMs - dateMs) / YEAR);
	if (secondsAgo < 60) {
		return 'just now';
	}
	if (minutesAgo < 60) {
		return rtf.format(minutesAgo * -1, 'minute');
	}
	if (hoursAgo < 24) {
		return rtf.format(hoursAgo * -1, 'hour');
	}
	if (daysAgo === 1) {
		return 'yesterday';
	}
	if (daysAgo < 7) {
		return rtf.format(daysAgo * -1, 'day');
	}
	if (daysAgo < 30) {
		return rtf.format(weeksAgo * -1, 'week');
	}
	if (daysAgo < 335) {
		// show 1 year ago instead of 12 months ago
		return rtf.format(monthsAgo * -1, 'month');
	}
	return rtf.format(yearsAgo * -1, 'year');
}

export default function useRelativeTimeString(date: Date) {
	const [timeString, setTimeString] = React.useState(relativeTimeString(date));
	const maybeSetDateString = React.useCallback(() => {
		const nextTimeString = relativeTimeString(date);
		if (timeString === nextTimeString) return;
		setTimeString(nextTimeString);
	}, [date, timeString]);
	React.useEffect(() => {
		const ref = setInterval(() => {
			maybeSetDateString();
		}, 1 * MINUTE);
		return () => {
			clearInterval(ref);
		};
	}, [maybeSetDateString]);
	return timeString;
}
