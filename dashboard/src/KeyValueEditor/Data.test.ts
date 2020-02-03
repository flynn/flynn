import {
	buildData,
	filterData,
	mapEntries,
	getEntries,
	MapEntriesOption,
	Entry,
	setKeyAtIndex,
	appendKey,
	setValueAtIndex,
	appendValue,
	appendEntry,
	removeEntryAtIndex,
	entriesDiff,
	rebaseData
} from './Data';

it('buildData sets correct length and hasChanges', () => {
	const a = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);

	expect(a.length).toEqual(3);
	expect(a.hasChanges).toEqual(false);
});

it('mapEntries iterates over entries', () => {
	const a = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);

	expect(
		mapEntries(a, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['second', 'second-val', 1],
		['third', 'third-val', 2]
	]);
});

it('mapEntries with APPEND_EMPTY_ENTRY iterates over entries with an extra empty one at end', () => {
	const a = buildData([['first', 'first-val']]);

	expect(
		mapEntries(
			a,
			([key, val]: Entry, index: number) => {
				return [key, val, index];
			},
			MapEntriesOption.APPEND_EMPTY_ENTRY
		)
	).toEqual([
		['first', 'first-val', 0],
		['', '', 1]
	]);
});

it('filterData returns Data with filter applied for mapEntires and getEntries', () => {
	const a = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);
	const filtered = filterData(a, 'ir');

	expect(
		mapEntries(filtered, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', 'third-val', 2]
	]);

	expect(getEntries(filtered)).toEqual([
		['first', 'first-val'],
		['third', 'third-val']
	]);
});

it('setKeyAtIndex sets entry key at given index', () => {
	const a = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);

	const b = setKeyAtIndex(a, 'two', 1);
	expect(b.length).toEqual(a.length);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['two', 'second-val', 1],
		['third', 'third-val', 2]
	]);
});

it('setKeyAtIndex removes existing entry with given key', () => {
	const a = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);

	const b = setKeyAtIndex(a, 'first', 1);
	expect(b.length).toEqual(a.length);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'second-val', 1],
		['third', 'third-val', 2]
	]);
});

it('setKeyAtIndex correctly identifies key to remove', () => {
	const a = buildData([
		['one.one', '1.1'],
		['one.two', '1.2']
	]);

	const b = appendKey(a, 'one.');
	expect(b.length).toEqual(a.length + 1);
	expect(b.hasChanges).toEqual(true);

	const c = setKeyAtIndex(b, 'one.three', 2);
	expect(c.length).toEqual(b.length);
	expect(c.hasChanges).toEqual(true);

	const d = appendKey(c, 'one.');
	expect(
		mapEntries(d, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['one.one', '1.1', 0],
		['one.two', '1.2', 1],
		['one.three', '', 2],
		['one.', '', 3]
	]);
	expect(d.length).toEqual(c.length + 1);
	expect(d.hasChanges).toEqual(true);
});

it('setKeyAtIndex appends entry with given key if index is _entries.length', () => {
	const a = buildData([['first', 'first-val']]);

	const b = setKeyAtIndex(a, 'two', 1);
	expect(b.length).toEqual(a.length + 1);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['two', '', 1]
	]);
});

it('setKeyAtIndex removes entry if given key is empty and value is also empty', () => {
	const a = setKeyAtIndex(
		buildData([
			['first', 'first-val'],
			['second', ''],
			['third', 'third-val']
		]),
		'',
		1
	);
	expect(
		mapEntries(a, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', 'third-val', 2]
	]);
	expect(a.length).toEqual(2);
	expect(a.hasChanges).toEqual(true);

	const b = setKeyAtIndex(
		buildData([
			['first', 'first-val'],
			['second', 'second-val'],
			['third', 'third-val']
		]),
		'',
		1
	);
	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['', 'second-val', 1],
		['third', 'third-val', 2]
	]);
	expect(b.length).toEqual(3);
	expect(b.hasChanges).toEqual(true);
});

it('setKeyAtIndex changing key then changing back results in no changes', () => {
	const a = setKeyAtIndex(setKeyAtIndex(buildData([['first', 'first-val']]), 'other', 0), 'first', 0);
	expect(
		mapEntries(a, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', 'first-val', 0]]);
	expect(a.length).toEqual(1);
	expect(a.hasChanges).toEqual(false);

	// append key of same name
	const b = setKeyAtIndex(setKeyAtIndex(appendEntry(a, 'first', 'first-val'), 'other', 1), 'first', 1);
	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', 'first-val', 1]]);
	expect(b.length).toEqual(1);
	expect(b.hasChanges).toEqual(false);
});

it('setKeyAtIndex throws an error if index out of bounds', () => {
	const a = buildData([['first', 'first-val']]);

	const errorMatcher = /out of bounds/;
	expect(() => setKeyAtIndex(a, 'two', 2)).toThrowError(errorMatcher);
	expect(() => setKeyAtIndex(a, 'two', 3)).toThrowError(errorMatcher);
	expect(() => setKeyAtIndex(a, 'two', -1)).toThrowError(errorMatcher);
	expect(() => setKeyAtIndex(a, 'two', -2)).toThrowError(errorMatcher);
});

it('appendKey appends entry with given key', () => {
	const a = buildData([['first', 'first-val']]);

	const b = appendKey(a, 'two');
	expect(b.length).toEqual(a.length + 1);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['two', '', 1]
	]);
});

it('setValueAtIndex sets entry value at given index', () => {
	const a = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);

	const b = setValueAtIndex(a, '3', 2);
	expect(b.length).toEqual(a.length);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['second', 'second-val', 1],
		['third', '3', 2]
	]);
});

it('setValueAtIndex removes entry if key and given value are empty', () => {
	const a = setValueAtIndex(
		buildData([
			['first', 'first-val'],
			['', 'second-val'],
			['third', 'third-val']
		]),
		'',
		1
	);
	expect(
		mapEntries(a, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', 'third-val', 2]
	]);
	expect(a.length).toEqual(2);
	expect(a.hasChanges).toEqual(true);

	const b = setValueAtIndex(
		buildData([
			['first', 'first-val'],
			['second', 'second-val'],
			['third', 'third-val']
		]),
		'',
		1
	);
	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['second', '', 1],
		['third', 'third-val', 2]
	]);
	expect(b.length).toEqual(3);
	expect(b.hasChanges).toEqual(true);
});

it('setValueAtIndex appends entry with given value if index is _entries.length', () => {
	const a = buildData([['first', 'first-val']]);

	const b = setValueAtIndex(a, '2', 1);
	expect(b.length).toEqual(a.length + 1);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['', '2', 1]
	]);
});

it('setValueAtIndex changing value then changing back results in no changes', () => {
	const a = setValueAtIndex(setValueAtIndex(buildData([['first', 'first-val']]), 'other', 0), 'first-val', 0);
	expect(
		mapEntries(a, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', 'first-val', 0]]);
	expect(a.length).toEqual(1);
	expect(a.hasChanges).toEqual(false);
});

it('setValueAtIndex throws an error if index out of bounds', () => {
	const a = buildData([['first', 'first-val']]);

	const errorMatcher = /out of bounds/;
	expect(() => setValueAtIndex(a, '2', 2)).toThrowError(errorMatcher);
	expect(() => setValueAtIndex(a, '2', 3)).toThrowError(errorMatcher);
	expect(() => setValueAtIndex(a, '2', -1)).toThrowError(errorMatcher);
	expect(() => setValueAtIndex(a, '2', -2)).toThrowError(errorMatcher);
});

it('appendValue appends entry with given value', () => {
	const a = buildData([['first', 'first-val']]);

	const b = appendValue(a, '2');
	expect(b.length).toEqual(a.length + 1);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['', '2', 1]
	]);
});

it('appendEntry appends entry with given key and value', () => {
	const a = buildData([['first', 'first-val']]);

	const b = appendEntry(a, 'two', '2');
	expect(b.length).toEqual(a.length + 1);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['two', '2', 1]
	]);
});

it('removeEntryAtIndex removes entry at given index', () => {
	const a = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);

	const b = removeEntryAtIndex(a, 1);
	expect(b.length).toEqual(a.length - 1);
	expect(b.hasChanges).toEqual(true);

	expect(
		mapEntries(b, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', 'third-val', 2]
	]);
});

it('removeEntryAtIndex throws an error if index out of bounds', () => {
	const a = buildData([['first', 'first-val']]);

	const errorMatcher = /out of bounds/;
	expect(() => removeEntryAtIndex(a, 1)).toThrowError(errorMatcher);
	expect(() => removeEntryAtIndex(a, 2)).toThrowError(errorMatcher);
	expect(() => removeEntryAtIndex(a, -1)).toThrowError(errorMatcher);
	expect(() => removeEntryAtIndex(a, -2)).toThrowError(errorMatcher);
});

it('mapEntries with DELETED_ONLY iterates over deleted entries', () => {
	let data = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);
	data = removeEntryAtIndex(data, 1);
	data = removeEntryAtIndex(data, 0);

	expect(
		mapEntries(
			data,
			([key, val]: Entry, index: number) => {
				return [key, val, index];
			},
			MapEntriesOption.DELETED_ONLY
		)
	).toEqual([
		['first', 'first-val', 0],
		['second', 'second-val', 1]
	]);
});

it('mapEntries with DELETED_ONLY iterates over deleted entries but not replaced ones', () => {
	let data = buildData([['first', 'first-val']]);
	data = removeEntryAtIndex(data, 0);
	data = setValueAtIndex(data, 'first-val-2', 1);
	data = setKeyAtIndex(data, 'first', 1);
	data = removeEntryAtIndex(data, 1);

	expect(
		mapEntries(
			data,
			([key, val]: Entry, index: number) => {
				return [key, val, index];
			},
			MapEntriesOption.DELETED_ONLY
		)
	).toEqual([['first', 'first-val-2', 1]]);
});

it('mulitple edits work as expected', () => {
	let data = buildData([['first', 'first-val']]);

	data = appendKey(data, 'second');
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['second', '', 1]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(2);

	data = appendKey(data, 'third');
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['second', '', 1],
		['third', '', 2]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(3);

	data = appendValue(data, '4');
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['second', '', 1],
		['third', '', 2],
		['', '4', 3]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(4);

	data = removeEntryAtIndex(data, 1);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', '', 2],
		['', '4', 3]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(3);

	data = setValueAtIndex(data, '3', 2);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', '3', 2],
		['', '4', 3]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(3);

	data = setKeyAtIndex(data, 'fourth', 3);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', '3', 2],
		['fourth', '4', 3]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(3);

	data = setValueAtIndex(data, 'four', 3);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', '3', 2],
		['fourth', 'four', 3]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(3);

	data = appendEntry(data, 'fourth', '4');
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', '3', 2],
		['fourth', '4', 4]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(3);

	data = appendEntry(data, 'fourth', '');
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['first', 'first-val', 0],
		['third', '3', 2],
		['fourth', '', 5]
	]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(3);

	data = removeEntryAtIndex(data, 2);
	data = removeEntryAtIndex(data, 5);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', 'first-val', 0]]);
	expect(data.hasChanges).toEqual(false);
	expect(data.length).toEqual(1);

	data = appendEntry(data, 'first', '');
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', '', 6]]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(1);

	data = setKeyAtIndex(data, 'first', 7);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', '', 7]]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(1);

	data = setKeyAtIndex(data, 'one', 7);
	data = setValueAtIndex(data, '1', 7);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['one', '1', 7]]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(1);

	data = setKeyAtIndex(data, 'first', 7);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', '1', 7]]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(1);
});

it('entriesDiff returns a Diff indicating any and all changes made to given data', () => {
	let data = buildData([['first', 'first-val']]);

	// no changes
	expect(entriesDiff(data)).toEqual([{ op: 'keep', prev: ['first', 'first-val'] }]);

	// change key
	data = setKeyAtIndex(data, 'one', 0);
	expect(entriesDiff(data)).toEqual([{ op: 'replace', prev: ['first', 'first-val'], next: ['one', 'first-val'] }]);

	// change value
	data = setValueAtIndex(data, '1', 0);
	expect(entriesDiff(data)).toEqual([{ op: 'replace', prev: ['first', 'first-val'], next: ['one', '1'] }]);

	// append key
	data = appendKey(data, 'second');
	expect(entriesDiff(data)).toEqual([
		{ op: 'replace', prev: ['first', 'first-val'], next: ['one', '1'] },
		{ op: 'add', next: ['second', ''] }
	]);

	// append value
	data = appendValue(data, '3rd');
	expect(entriesDiff(data)).toEqual([
		{ op: 'replace', prev: ['first', 'first-val'], next: ['one', '1'] },
		{ op: 'add', next: ['second', ''] },
		{ op: 'add', next: ['', '3rd'] }
	]);

	// set key for appended value
	data = setKeyAtIndex(data, 'third', 2);
	expect(entriesDiff(data)).toEqual([
		{ op: 'replace', prev: ['first', 'first-val'], next: ['one', '1'] },
		{ op: 'add', next: ['second', ''] },
		{ op: 'add', next: ['third', '3rd'] }
	]);

	// set value for appended key
	data = setValueAtIndex(data, '2nd', 1);
	expect(entriesDiff(data)).toEqual([
		{ op: 'replace', prev: ['first', 'first-val'], next: ['one', '1'] },
		{ op: 'add', next: ['second', '2nd'] },
		{ op: 'add', next: ['third', '3rd'] }
	]);

	// remove entry
	data = removeEntryAtIndex(data, 0);
	expect(entriesDiff(data)).toEqual([
		{ op: 'remove', prev: ['first', 'first-val'] },
		{ op: 'add', next: ['second', '2nd'] },
		{ op: 'add', next: ['third', '3rd'] }
	]);

	// remove appended entry
	data = removeEntryAtIndex(data, 1);
	expect(entriesDiff(data)).toEqual([
		{ op: 'remove', prev: ['first', 'first-val'] },
		{ op: 'add', next: ['third', '3rd'] }
	]);

	// remove other appended entry
	data = removeEntryAtIndex(data, 2);
	expect(entriesDiff(data)).toEqual([{ op: 'remove', prev: ['first', 'first-val'] }]);

	// add original entry back
	data = appendKey(data, 'first');
	data = setValueAtIndex(data, 'first-val', 3);
	expect(entriesDiff(data)).toEqual([{ op: 'keep', prev: ['first', 'first-val'] }]);

	// change original entry
	data = setKeyAtIndex(data, 'one', 3);
	data = setValueAtIndex(data, '1', 3);
	expect(entriesDiff(data)).toEqual([{ op: 'replace', prev: ['first', 'first-val'], next: ['one', '1'] }]);

	// remove original entry again
	data = removeEntryAtIndex(data, 3);
	expect(entriesDiff(data)).toEqual([{ op: 'remove', prev: ['first', 'first-val'] }]);
});

it('rebaseData attempts to apply changes to given base and produces a list of conflicts', () => {
	let data = buildData([
		['first', 'first-val'],
		['second', 'second-val'],
		['third', 'third-val']
	]);

	const originalData = data;
	let base = [
		['one', '1'],
		['second', '2nd'],
		['third', 'third-val'],
		['four', '4']
	] as [string, string][];

	// test with no changes
	const data2 = rebaseData(data, base);
	expect(
		mapEntries(data2, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['second', '2nd', 1],
		['third', 'third-val', 2],
		['one', '1', 3],
		['four', '4', 4]
	]);
	expect(data2.hasChanges).toEqual(false);
	expect(data2.length).toEqual(base.length);
	expect(data2.conflicts).toEqual([]);

	// test with non-conflicting changes
	data = setValueAtIndex(originalData, '3rd', 2);
	const data3 = rebaseData(data, base);
	expect(
		mapEntries(data3, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['second', '2nd', 1],
		['third', '3rd', 2],
		['one', '1', 3],
		['four', '4', 4]
	]);
	expect(data3.hasChanges).toEqual(true);
	expect(data3.length).toEqual(base.length);
	expect(data3.conflicts).toEqual([]);

	// test with non-conflicting changes
	data = setKeyAtIndex(originalData, 'three', 2);
	data = setValueAtIndex(data, '3', 2);
	const data4 = rebaseData(data, base);
	expect(
		mapEntries(data4, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['second', '2nd', 1],
		['three', '3', 2],
		['one', '1', 3],
		['four', '4', 4]
	]);
	expect(data4.hasChanges).toEqual(true);
	expect(data4.length).toEqual(base.length);
	expect(data4.conflicts).toEqual([]);

	// test with non-conflicting changes (add key/val that is also added in base)
	data = setKeyAtIndex(originalData, 'one', 0);
	data = setValueAtIndex(data, '1', 0);
	const data5 = rebaseData(data, base);
	expect(
		mapEntries(data5, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['one', '1', 0],
		['second', '2nd', 1],
		['third', 'third-val', 2],
		['four', '4', 3]
	]);
	expect(data5.hasChanges).toEqual(false);
	expect(data5.length).toEqual(base.length);
	expect(data5.conflicts).toEqual([]);

	// test with non-conflicting changes (add key/val that is also added in base)
	data = appendKey(originalData, 'four');
	data = setValueAtIndex(data, '4', 3);
	const data6 = rebaseData(data, base);
	expect(
		mapEntries(data6, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['second', '2nd', 1],
		['third', 'third-val', 2],
		['four', '4', 3],
		['one', '1', 4]
	]);
	expect(data6.hasChanges).toEqual(false);
	expect(data6.length).toEqual(base.length);
	expect(data6.conflicts).toEqual([]);

	// test with non-conflicting changes (add new key/val that is not in base)
	data = appendKey(originalData, 'five');
	data = setValueAtIndex(data, '5', 3);
	const data7 = rebaseData(data, base);
	expect(
		mapEntries(data7, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['second', '2nd', 1],
		['third', 'third-val', 2],
		['five', '5', 3],
		['one', '1', 4],
		['four', '4', 5]
	]);
	expect(data7.hasChanges).toEqual(true);
	expect(data7.length).toEqual(base.length + 1);
	expect(data7.conflicts).toEqual([]);

	// test with conflicting changes (add new key that is also added in base, but
	// use a different val)
	data = appendKey(originalData, 'four');
	data = setValueAtIndex(data, 'four', 3);
	const data8 = rebaseData(data, base);
	expect(
		mapEntries(data8, ([key, val, { rebaseConflict }]: Entry, index: number) => {
			return [key, val, index, !!rebaseConflict];
		})
	).toEqual([
		['second', '2nd', 1, false],
		['third', 'third-val', 2, false],
		['four', 'four', 3, true],
		['one', '1', 4, false]
	]);
	expect(data8.hasChanges).toEqual(true);
	expect(data8.length).toEqual(base.length);
	expect(data8.conflicts).toEqual([
		[
			{ op: 'add', next: ['four', '4'] },
			{ op: 'add', next: ['four', 'four'] }
		]
	]);

	// test resolving conflict
	data = setValueAtIndex(data8, '4', 3);
	expect(
		mapEntries(data, ([key, val, { rebaseConflict }]: Entry, index: number) => {
			return [key, val, index, !!rebaseConflict];
		})
	).toEqual([
		['second', '2nd', 1, false],
		['third', 'third-val', 2, false],
		['four', '4', 3, false],
		['one', '1', 4, false]
	]);
	expect(data.conflicts).toEqual([]);
	expect(data.length).toEqual(base.length);
	expect(data.hasChanges).toEqual(false);

	// test resolving conflict a different way
	data = setKeyAtIndex(data8, 'four', 5);
	data = setValueAtIndex(data, '4', 5);
	expect(
		mapEntries(data, ([key, val, { rebaseConflict }]: Entry, index: number) => {
			return [key, val, index, !!rebaseConflict];
		})
	).toEqual([
		['second', '2nd', 1, false],
		['third', 'third-val', 2, false],
		['one', '1', 4, false],
		['four', '4', 5, false]
	]);
	expect(data.conflicts).toEqual([]);
	expect(data.length).toEqual(base.length);
	expect(data.hasChanges).toEqual(false);

	// test with conflicting changes (change key to one that is also added in
	// base, but use a different val)
	data = setKeyAtIndex(originalData, 'four', 2);
	data = setValueAtIndex(data, 'four', 2);
	const data9 = rebaseData(data, base);
	expect(
		mapEntries(data9, ([key, val, { rebaseConflict }]: Entry, index: number) => {
			return [key, val, index, !!rebaseConflict];
		})
	).toEqual([
		['second', '2nd', 1, false],
		['four', 'four', 2, true],
		['one', '1', 3, false]
	]);
	expect(data9.hasChanges).toEqual(true);
	expect(data9.length).toEqual(base.length - 1);
	expect(data9.conflicts).toEqual([
		[
			{ op: 'add', next: ['four', '4'] },
			{ op: 'replace', prev: ['third', 'third-val'], next: ['four', 'four'] }
		]
	]);

	// test resolving conflict
	data = setValueAtIndex(data9, '4', 2);
	expect(
		mapEntries(data, ([key, val, { rebaseConflict }]: Entry, index: number) => {
			return [key, val, index, !!rebaseConflict];
		})
	).toEqual([
		['second', '2nd', 1, false],
		['four', '4', 2, false],
		['one', '1', 3, false]
	]);
	expect(data.conflicts).toEqual([]);
	expect(data.length).toEqual(base.length - 1);
	expect(data.hasChanges).toEqual(false);

	// test with non-conflicting changes (change key/val to one that is also
	// added in base)
	data = setKeyAtIndex(originalData, 'four', 2);
	data = setValueAtIndex(data, '4', 2);
	const data10 = rebaseData(data, base);
	expect(
		mapEntries(data10, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['second', '2nd', 1],
		['four', '4', 2],
		['one', '1', 3]
	]);
	expect(data10.hasChanges).toEqual(true);
	expect(data10.length).toEqual(base.length - 1);
	expect(data10.conflicts).toEqual([]);

	// test with non-conflicting changes (remove entry that is unchanged in base)
	data = removeEntryAtIndex(originalData, 2);
	const data11 = rebaseData(data, base);
	expect(
		mapEntries(data11, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['second', '2nd', 1],
		['one', '1', 3],
		['four', '4', 4]
	]);
	expect(data11.hasChanges).toEqual(true);
	expect(data11.length).toEqual(base.length - 1);
	expect(data11.conflicts).toEqual([]);

	// test with conflicting changes (remove entry that is changed in base)
	data = removeEntryAtIndex(originalData, 1);
	const data12 = rebaseData(data, base);
	expect(
		mapEntries(data12, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['third', 'third-val', 2],
		['one', '1', 3],
		['four', '4', 4]
	]);
	expect(data12.hasChanges).toEqual(true);
	expect(data12.length).toEqual(base.length - 1);
	expect(data12.conflicts).toEqual([
		[
			{ op: 'replace', prev: ['second', 'second-val'], next: ['second', '2nd'] },
			{ op: 'remove', prev: ['second', 'second-val'] }
		]
	]);

	// test resolving conflict
	data = appendEntry(data12, 'second', '2nd');
	expect(
		mapEntries(data, ([key, val, { rebaseConflict }]: Entry, index: number) => {
			return [key, val, index, !!rebaseConflict];
		})
	).toEqual([
		['third', 'third-val', 2, false],
		['one', '1', 3, false],
		['four', '4', 4, false],
		['second', '2nd', 5, false]
	]);
	expect(data.conflicts).toEqual([]);
	expect(data.length).toEqual(base.length);
	expect(data.hasChanges).toEqual(false);

	// test making a change
	data = appendKey(data, 'hello');
	expect(
		mapEntries(data, ([key, val, { rebaseConflict }]: Entry, index: number) => {
			return [key, val, index, !!rebaseConflict];
		})
	).toEqual([
		['third', 'third-val', 2, false],
		['one', '1', 3, false],
		['four', '4', 4, false],
		['second', '2nd', 5, false],
		['hello', '', 6, false]
	]);
	expect(data.conflicts).toEqual([]);
	expect(data.length).toEqual(base.length + 1);
	expect(data.hasChanges).toEqual(true);

	// test removing change
	data = removeEntryAtIndex(data, 6);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([
		['third', 'third-val', 2],
		['one', '1', 3],
		['four', '4', 4],
		['second', '2nd', 5]
	]);
	expect(data.conflicts).toEqual([]);
	expect(data.length).toEqual(base.length);
	expect(data.hasChanges).toEqual(false);

	// test with non-conflicting changes (change then remove entry that is removed in base)
	data = setValueAtIndex(buildData([['one', '1']]), '2', 0);
	data = removeEntryAtIndex(data, 0);
	const data13 = rebaseData(data, []);
	expect(
		mapEntries(data13, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([]);
	expect(data13.hasChanges).toEqual(false);
	expect(data13.length).toEqual(0);
	expect(data13.conflicts).toEqual([]);

	// test making some changes
	data = appendEntry(data, 'one', 'ONE');
	data = removeEntryAtIndex(data, 1);
	expect(
		mapEntries(data13, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([]);
	expect(data13.hasChanges).toEqual(false);
	expect(data13.length).toEqual(0);
	expect(data13.conflicts).toEqual([]);

	// test with non-conflicting changes (change that is removed in base)
	data = setValueAtIndex(buildData([['one', '1']]), '2', 0);
	const data14 = rebaseData(data, []);
	expect(
		mapEntries(data14, ([key, val, { originalKey, originalValue }]: Entry, index: number) => {
			return [[key, val], [originalKey, originalValue], index];
		})
	).toEqual([[['one', '2'], [undefined, undefined], 0]]);
	expect(data14.hasChanges).toEqual(true);
	expect(data14.length).toEqual(1);
	expect(data14.conflicts).toEqual([]);
});

it('setting entry multiple times then rebasing with conflict on that entry can resolve conflict', () => {
	let data = buildData([['first', 'first-val']]);
	data = setKeyAtIndex(data, 'first', 1);
	data = setValueAtIndex(data, 'first-val-2', 1);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(1);

	let base = [['first', '1']] as [string, string][];
	data = rebaseData(data, base);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', 'first-val-2', 1]]);
	expect(data.hasChanges).toEqual(true);
	expect(data.length).toEqual(1);
	expect(data.conflicts).toEqual([
		[
			{ op: 'add', next: ['first', '1'] },
			{ op: 'replace', prev: ['first', 'first-val'], next: ['first', 'first-val-2'] }
		]
	]);

	data = setValueAtIndex(data, '1', 1);
	expect(
		mapEntries(data, ([key, val]: Entry, index: number) => {
			return [key, val, index];
		})
	).toEqual([['first', '1', 1]]);
	expect(data.hasChanges).toEqual(false);
	expect(data.length).toEqual(1);
	expect(data.conflicts).toEqual([]);
});
