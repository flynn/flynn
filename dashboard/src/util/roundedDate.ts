export default function roundedDate(d: Date): Date {
	const out = new Date(d);
	out.setMilliseconds(0);
	out.setSeconds(0);
	out.setMinutes(0);
	out.setHours(0);
	return out;
}
