import * as jspb from 'google-protobuf';
import protoMapDiff, { Diff, applyProtoMapDiff, mergeProtoMapDiff, DiffOption } from './protoMapDiff';
import protoMapToObject from './protoMapToObject';

it('generates diff', () => {
	const a = new jspb.Map([
		['first', 'first-value'],
		['third', 'third-value-1']
	]);
	const b = new jspb.Map([
		['second', 'second-value'],
		['third', 'third-value-2']
	]);
	const diff = protoMapDiff(a, b);
	expect(diff).toEqual([
		{ op: 'remove', key: 'first' },
		{ op: 'remove', key: 'third' },
		{ op: 'add', key: 'third', value: 'third-value-2' },
		{ op: 'add', key: 'second', value: 'second-value' }
	]);
});

it('generates diff including inchanged properties', () => {
	const a = new jspb.Map([
		['first', 'first-value'],
		['third', 'third-value-1'],
		['fourth', 'fourth-value']
	]);
	const b = new jspb.Map([
		['second', 'second-value'],
		['third', 'third-value-2'],
		['fourth', 'fourth-value']
	]);
	const diff = protoMapDiff(a, b, DiffOption.INCLUDE_UNCHANGED);
	expect(diff).toEqual([
		{ op: 'remove', key: 'first' },
		{ op: 'keep', key: 'fourth' },
		{ op: 'remove', key: 'third' },
		{ op: 'add', key: 'third', value: 'third-value-2' },
		{ op: 'add', key: 'second', value: 'second-value' }
	]);
});

it('applies diff', () => {
	const a = new jspb.Map([
		['first', 'first-value'],
		['third', 'third-value-1'],
		['fourth', 'fourth-value']
	]);
	const diff = [
		{ op: 'add', key: 'second', value: 'second-value' },
		{ op: 'remove', key: 'first' },
		{ op: 'remove', key: 'third' },
		{ op: 'add', key: 'third', value: 'third-value-2' }
	] as Diff<string, string>;
	const b = applyProtoMapDiff(a, diff);
	expect(protoMapToObject(b)).toEqual({ second: 'second-value', third: 'third-value-2', fourth: 'fourth-value' });
	expect(protoMapToObject(a)).toEqual({
		first: 'first-value',
		third: 'third-value-1',
		fourth: 'fourth-value'
	});
});

it('applies diff via mutation', () => {
	const a = new jspb.Map([
		['first', 'first-value'],
		['third', 'third-value-1'],
		['fourth', 'fourth-value']
	]);
	const diff = [
		{ op: 'add', key: 'second', value: 'second-value' },
		{ op: 'remove', key: 'first' },
		{ op: 'remove', key: 'third' },
		{ op: 'add', key: 'third', value: 'third-value-2' }
	] as Diff<string, string>;
	const b = applyProtoMapDiff(a, diff, true);
	expect(protoMapToObject(b)).toEqual({
		second: 'second-value',
		third: 'third-value-2',
		fourth: 'fourth-value'
	});
	expect(protoMapToObject(a)).toEqual({
		second: 'second-value',
		third: 'third-value-2',
		fourth: 'fourth-value'
	});
});

it('merges diffs', () => {
	const a = [
		{ op: 'add', key: 'second', value: 'second-value' },
		{ op: 'add', key: 'fifth', value: 'fifth-value' },
		{ op: 'remove', key: 'first' },
		{ op: 'remove', key: 'third' }
	] as Diff<string, string>;
	const b = [
		{ op: 'remove', key: 'first' },
		{ op: 'remove', key: 'third' },
		{ op: 'remove', key: 'second' },
		{ op: 'add', key: 'third', value: 'third-value-2' },
		{ op: 'add', key: 'second', value: 'second-value-2' }
	] as Diff<string, string>;
	const [c, conflicts, conflictKeys] = mergeProtoMapDiff(a, b);
	expect(c).toEqual([
		{ op: 'add', key: 'second', value: 'second-value-2' },
		{ op: 'remove', key: 'first' },
		{ op: 'add', key: 'fifth', value: 'fifth-value' },
		{ op: 'add', key: 'third', value: 'third-value-2' }
	]);
	expect(conflicts).toEqual([
		[
			{ op: 'add', key: 'second', value: 'second-value' },
			{ op: 'add', key: 'second', value: 'second-value-2' }
		]
	]);
	expect([...conflictKeys]).toEqual(['second']);
});
