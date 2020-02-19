import roundedDate from './util/roundedDate';

export const SECOND = 1000;
export const MINUTE = SECOND * 60;
export const HOUR = MINUTE * 60;
export const DAY = HOUR * 24;
export const WEEK = DAY * 7;
export const MONTH = DAY * 30;
export const YEAR = DAY * 365;

export function isToday(d: Date): boolean {
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

export function isYesterday(d: Date): boolean {
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
