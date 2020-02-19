import * as React from 'react';
import useRelativeTimeString from './useRelativeTimeString';

export interface Props {
	date: Date;
}

export default function TimeAgo({ date }: Props) {
	const relativeTimeString = useRelativeTimeString(date);
	return (
		<time dateTime={date.toLocaleString()} title={date.toLocaleString()}>
			{relativeTimeString}
		</time>
	);
}
