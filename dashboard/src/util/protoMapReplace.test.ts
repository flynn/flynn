import * as jspb from 'google-protobuf';
import protoMapReplace from './protoMapReplace';

it('replaces map properties', () => {
	const a = new jspb.Map([
		['first', 'first-value'],
		['third', 'third-value-1']
	]);
	const b = new jspb.Map([
		['second', 'second-value'],
		['third', 'third-value-2']
	]);
	protoMapReplace(a, b);
	expect(a.toArray()).toEqual([
		['second', 'second-value'],
		['third', 'third-value-2']
	]);
});
