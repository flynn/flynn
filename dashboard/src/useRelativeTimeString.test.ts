import { relativeTimeString } from './useRelativeTimeString';
import { SECOND, MINUTE, HOUR, DAY, WEEK, MONTH, YEAR } from './dateCommon';

it('outputs seconds as just now', () => {
	expect(relativeTimeString(new Date(Date.now() - SECOND * 45))).toEqual('just now');
});

it('outputs minutes', () => {
	expect(relativeTimeString(new Date(Date.now() - MINUTE * 45))).toEqual('45 minutes ago');
});

it('outputs hours', () => {
	expect(relativeTimeString(new Date(Date.now() - HOUR * 23))).toEqual('23 hours ago');
});

it('outputs 1 day ago as yesterday', () => {
	expect(relativeTimeString(new Date(Date.now() - DAY * 1))).toEqual('yesterday');
});

it('outputs days', () => {
	expect(relativeTimeString(new Date(Date.now() - DAY * 6))).toEqual('6 days ago');
});

it('outputs weeks', () => {
	expect(relativeTimeString(new Date(Date.now() - WEEK * 1))).toEqual('1 week ago');
	expect(relativeTimeString(new Date(Date.now() - DAY * 10))).toEqual('1 week ago');
	expect(relativeTimeString(new Date(Date.now() - WEEK * 3))).toEqual('3 weeks ago');
});

it('outputs months', () => {
	expect(relativeTimeString(new Date(Date.now() - MONTH * 1))).toEqual('1 month ago');
	expect(relativeTimeString(new Date(Date.now() - MONTH * 2))).toEqual('2 months ago');
	expect(relativeTimeString(new Date(Date.now() - MONTH * 4))).toEqual('4 months ago');
	expect(relativeTimeString(new Date(Date.now() - MONTH * 6))).toEqual('6 months ago');
	expect(relativeTimeString(new Date(Date.now() - (MONTH * 11 - DAY * 15)))).toEqual('11 months ago');
});

it('outputs years', () => {
	expect(relativeTimeString(new Date(Date.now() - YEAR))).toEqual('1 year ago');
	expect(relativeTimeString(new Date(Date.now() - (MONTH * 11 + DAY * 16)))).toEqual('1 year ago');
	expect(relativeTimeString(new Date(Date.now() - YEAR * 2))).toEqual('2 years ago');
});
