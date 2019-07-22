import fz from 'fz';

export enum DataActionType {
	SET_KEY_AT_INDEX = 'KVDATA__SET_KEY_AT_INDEX',
	SET_VAL_AT_INDEX = 'KVDATA__SET_VAL_AT_INDEX',
	REBASE = 'KVDATA_REBASE',
	APPEND_ENTRY = 'KVDATA__APPEND_ENTRY',
	APPLY_FILTER = 'KVDATA__APPLY_FILTER'
}

interface SetKeyAtIndexAction {
	type: DataActionType.SET_KEY_AT_INDEX;
	key: string;
	index: number;
}

interface SetValAtIndexAction {
	type: DataActionType.SET_VAL_AT_INDEX;
	value: string;
	index: number;
}

interface RebaseAction {
	type: DataActionType.REBASE;
	base: [string, string][];
}

interface AppendEntryAction {
	type: DataActionType.APPEND_ENTRY;
	key: string;
	value: string;
}

interface ApplyFilterAction {
	type: DataActionType.APPLY_FILTER;
	query: string;
}

export type DataAction =
	| SetKeyAtIndexAction
	| SetValAtIndexAction
	| RebaseAction
	| AppendEntryAction
	| ApplyFilterAction;

export type DataReducer = (prevData: Data, action: DataAction | DataAction[]) => Data;

export function dataReducer(prevData: Data, actions: DataAction | DataAction[]): Data {
	if (!Array.isArray(actions)) actions = [actions];

	return actions.reduce((prevData: Data, action: DataAction) => {
		switch (action.type) {
			case DataActionType.SET_KEY_AT_INDEX:
				return setKeyAtIndex(prevData, action.key, action.index);
			case DataActionType.SET_VAL_AT_INDEX:
				return setValueAtIndex(prevData, action.value, action.index);
			case DataActionType.REBASE:
				return rebaseData(prevData, action.base);
			case DataActionType.APPEND_ENTRY:
				return appendEntry(prevData, action.key, action.value);
			case DataActionType.APPLY_FILTER:
				return filterData(prevData, action.query);
			default:
				throw new Error(`Unknown action: ${JSON.stringify(action)}`);
		}
	}, prevData);
}

export interface EntryState {
	originalKey?: string;
	originalValue?: string;
	deleted?: boolean;
	rebaseConflict?: [DiffEntry, DiffEntry];
	rebaseConflictIndex?: number;
}

export type Entry = [string, string, EntryState]; // key, val, state

export interface Data {
	_entries: Array<Entry | undefined>;
	_indices: Set<number>;
	_indicesMap: Map<string, number>;
	_changedIndices: Set<number>;
	_deletedLength: number;
	_filterPattern?: string;
	length: number;
	hasChanges: boolean;
	conflicts?: [DiffEntry, DiffEntry][];
	[Symbol.iterator](): Iterator<[Entry, number | undefined]>;
}

export interface DiffEntry {
	op: 'keep' | 'remove' | 'add' | 'replace';
	// keep is used when an `Entry`'s originalKey/originalValue is equal to it's
	// present key/val
	//
	// remove is used when an `Entry`'s state.deleted === true
	// add is used when an `Entry`'s originalKey is undefined
	//
	// replace is used when an `Entry`'s originalKey or originalValue is not
	// equal to it's present key/val

	prev: [string, string]; // key, val
	next: [string, string]; // key, val
}
export type Diff = DiffEntry[];

export function buildData(base: [string, string][]): Data {
	const _entries = [] as Entry[];
	const _indices = new Set<number>();
	const _indicesMap = new Map<string, number>();

	base.forEach(([key, val]: [string, string], index: number) => {
		_indices.add(index);
		_indicesMap.set(key, index);
		_entries.push([key, val, { originalKey: key, originalValue: val }]);
	});

	let data = {} as Data;
	return Object.assign(data, {
		[Symbol.iterator](): Iterator<[Entry, number | undefined]> {
			return buildIterator(this);
		},
		_entries,
		_indices,
		_indicesMap,
		_changedIndices: new Set<number>(),
		_deletedLength: 0,
		length: _entries.length,
		hasChanges: false
	});
}

export function filterData(data: Data, filterPattern: string): Data {
	if (!data._filterPattern && !filterPattern) return data;
	if (data._filterPattern === filterPattern) return data;
	return Object.assign({}, data, { _filterPattern: filterPattern });
}

export function hasIndex(data: Data, index: number): boolean {
	return (data._entries[index] || false) && !(data._entries[index] as Entry)[2].deleted;
}

export function hasKey(data: Data, key: string): boolean {
	const index = data._indicesMap.get(key);
	if (index === undefined) {
		return false;
	}
	return hasIndex(data, index);
}

export function nextIndex(data: Data, index: number): number {
	for (let i = index; i < data._entries.length; i++) {
		if (hasIndex(data, i)) return i;
	}
	return data._entries.length;
}

export function getKeyAtIndex(data: Data, index: number): string | undefined {
	if (!hasIndex(data, index)) {
		return undefined;
	}
	return (data._entries[index] as Entry)[0];
}

export function setKeyAtIndex(data: Data, key: string, index: number): Data {
	if (index === data._entries.length) {
		if (key.trim() === '') {
			return data;
		}
		return appendKey(data, key);
	}

	if (!data._indices.has(index)) throw new Error(`setKeyAtIndex Error: index "${index}" out of bounds`);

	let [prevKey, val, state] = data._entries[index] as Entry;
	if (key.length === 0 && val.length === 0) {
		return removeEntryAtIndex(data, index);
	}

	// nothing to do, key is already set
	if (key === prevKey) return data;

	const originalIndex = data._indicesMap.has(key) ? data._indicesMap.get(key) : undefined;
	const [, , originalState = undefined] = originalIndex !== undefined ? data._entries[originalIndex] || [] : [];
	if (originalState) {
		// we're using the same key as an existing entry, so inherit it's state
		state = Object.assign({}, originalState);
		delete state.deleted;
	}

	const { originalKey, originalValue } = state;

	const nextData = Object.assign({}, data, {
		hasChanges: true,
		_indicesMap: new Map(data._indicesMap),
		_changedIndices: new Set(data._changedIndices)
	});

	if (key.length > 0) {
		nextData._indicesMap.delete(prevKey);
		nextData._indicesMap.set(key, index);
	}

	if (originalIndex !== undefined) {
		nextData._changedIndices.delete(originalIndex);
	}
	if (key === originalKey && val === originalValue) {
		// entry is at it's original state
		nextData._changedIndices.delete(index);
	} else {
		// entry is changed
		nextData._changedIndices.add(index);
	}
	nextData.hasChanges = nextData._changedIndices.size > 0;

	nextData._entries = data._entries
		.slice(0, index)
		.concat([[key, val, state]])
		.concat(data._entries.slice(index + 1));

	if (originalIndex !== undefined && originalIndex !== index) {
		// we're using the same key as an existing entry, so remove the existing entry
		nextData._entries[originalIndex] = undefined;
	}

	return nextData;
}

export function appendKey(data: Data, key: string): Data {
	const [, val, state] = ['', '', {} as EntryState];
	const nextData = Object.assign({}, data, { hasChanges: true, length: data.length + 1 });

	const originalIndex = data._indicesMap.has(key) ? data._indicesMap.get(key) : undefined;
	const [, value = undefined, originalState = undefined] =
		originalIndex !== undefined ? data._entries[originalIndex] || [] : [];
	if (originalState) {
		// we're using the same key as an existing entry, so inherit it's state
		Object.assign(state, originalState);
		delete state.deleted;
	}

	const index = data._entries.length;
	nextData._entries = data._entries.concat([[key, val, state]]);
	nextData._indices = new Set(data._indices);
	nextData._indices.add(index);
	if (key.length > 0) {
		nextData._indicesMap = new Map(nextData._indicesMap);
		nextData._indicesMap.set(key, index);
	}
	nextData._changedIndices = new Set(data._changedIndices);
	nextData._changedIndices.add(index);

	if (originalIndex !== undefined) {
		nextData._changedIndices.delete(originalIndex);
	}
	nextData._changedIndices.add(index);

	if (originalIndex !== undefined && originalState) {
		// we're using the same key as an existing entry, so remove the existing entry
		nextData._entries[originalIndex] = undefined;
		if (!originalState.deleted) {
			nextData.length--;
		}

		if (key === state.originalKey && value === state.originalValue) {
			// entry is at it's original state
			nextData._changedIndices.delete(index);
		}
	}

	nextData.hasChanges = nextData._changedIndices.size > 0;

	return nextData;
}

export function getValueAtIndex(data: Data, index: number): string | undefined {
	if (!hasIndex(data, index)) {
		return undefined;
	}
	return (data._entries[index] as Entry)[1];
}

export function setValueAtIndex(data: Data, value: string, index: number): Data {
	if (index === data._entries.length) {
		if (!value.trim()) {
			return data;
		}
		return appendValue(data, value);
	}
	if (!data._indices.has(index)) throw new Error(`setValueAtIndex Error: index "${index}" out of bounds`);

	let [key, , state] = data._entries[index] || ['', '', {}];
	if (key.length === 0 && value.length === 0) {
		return removeEntryAtIndex(data, index);
	}

	const rebaseConflictIndex = state.rebaseConflictIndex;
	if (state.rebaseConflict && value === state.originalValue) {
		state = Object.assign({}, state);
		delete state.rebaseConflict;
		delete state.rebaseConflictIndex;
	}

	const nextData = Object.assign({}, data, { hasChanges: true, _changedIndices: new Set(data._changedIndices) });

	if (key === state.originalKey && value === state.originalValue) {
		// entry is at it's original state
		nextData._changedIndices.delete(index);
	} else {
		// entry is changed
		nextData._changedIndices.add(index);
	}
	nextData.hasChanges = nextData._changedIndices.size > 0;

	nextData._entries = data._entries
		.slice(0, index)
		.concat([[key, value, state]])
		.concat(data._entries.slice(index + 1));

	if (rebaseConflictIndex !== undefined && nextData.conflicts) {
		nextData.conflicts = nextData.conflicts
			.slice(0, rebaseConflictIndex)
			.concat(nextData.conflicts.slice(rebaseConflictIndex + 1));
	}

	return nextData;
}

export function appendValue(data: Data, value: string): Data {
	const index = data._entries.length;
	const nextData = Object.assign({}, data, {
		hasChanges: true,
		_changedIndices: new Set(data._changedIndices),
		length: data.length + 1
	});
	const [key, , state] = ['', '', {}];
	nextData._entries = data._entries.concat([[key, value, state]]);
	nextData._indices = new Set(data._indices);
	nextData._indices.add(index);
	nextData._changedIndices.add(index);
	return nextData;
}

export function appendEntry(data: Data, key: string, value: string): Data {
	let nextData = appendKey(data, key);
	nextData = setValueAtIndex(nextData, value, nextData._entries.length - 1);
	return nextData;
}

export function removeEntryAtIndex(data: Data, index: number): Data {
	if (!data._indices.has(index)) throw new Error(`removeEntryAtIndex Error: index "${index}" out of bounds`);

	const nextData = Object.assign({}, data, {
		hasChanges: true,
		_changedIndices: new Set(data._changedIndices),
		length: data.length - 1,
		_deletedLength: data._deletedLength + 1
	});
	const [key, value, state] = data._entries[index] || ['', '', {}];

	if (state.originalKey === undefined && state.originalValue === undefined) {
		// entry is at it's original state of not existing
		nextData._changedIndices.delete(index);
	} else {
		// entry is changed
		nextData._changedIndices.add(index);
	}
	nextData.hasChanges = nextData._changedIndices.size > 0;

	nextData._entries = data._entries
		.slice(0, index)
		.concat([[key, value, Object.assign({}, state, { deleted: true })]])
		.concat(data._entries.slice(index + 1));
	return nextData;
}

export function getEntries(data: Data): [string, string][] {
	return mapEntries(data, ([key, value]: Entry) => {
		return [key, value] as [string, string];
	});
}

function buildIterator(data: Data): Iterator<[Entry, number | undefined]> {
	const entries = data._entries;
	let index: number = 0;
	const nextEntry = (): [Entry | null, number] => {
		if (index === entries.length) return [null, -1];
		const entry = entries[index++];
		if (entry === undefined) return nextEntry();
		return [entry, index - 1];
	};
	return {
		next: () => {
			const [entry, entryIndex] = nextEntry();
			if (entry) {
				return { value: [entry, entryIndex], done: false };
			}
			return { value: null, done: true };
		}
	};
}

export enum MapEntriesOption {
	APPEND_EMPTY_ENTRY,
	DELETED_ONLY
}

export function mapEntries<T>(data: Data, fn: (entry: Entry, index: number) => T, ...opts: MapEntriesOption[]): T[] {
	const _opts = new Set(opts);
	const deletedOnly = _opts.has(MapEntriesOption.DELETED_ONLY);
	const filterPattern = data._filterPattern;
	return data._entries
		.reduce((m: T[], entry: Entry | undefined, index: number) => {
			if (entry === undefined) return m;
			const [key, value, state] = entry;
			if ((deletedOnly && !state.deleted) || (!deletedOnly && state.deleted)) return m;
			if (filterPattern && !fz(key, filterPattern)) return m;
			m.push(fn([key, value, state], index));
			return m;
		}, [] as T[])
		.concat(_opts.has(MapEntriesOption.APPEND_EMPTY_ENTRY) ? [fn(['', '', {}], data._entries.length)] : []);
}

export function entriesDiff(data: Data): Diff {
	const diffMap = new Map<string, number>();
	return data._entries.reduce((m: Diff, entry: Entry | undefined, index: number) => {
		if (entry === undefined) return m;
		const [key, value, state] = entry;
		const hasChanges = !(key === state.originalKey && value === state.originalValue);
		if (state.deleted && state.originalKey !== undefined && state.originalValue !== undefined) {
			// check if it's already in the diff
			const ode = diffMap.has(state.originalKey || '') ? m[diffMap.get(state.originalKey || '') as number] : undefined;
			if (ode && ode.op === 'remove' && ode.prev[0] === state.originalKey && ode.prev[1] === state.originalValue) {
				return m;
			}

			// append remove op to diff for entry
			const de = { op: 'remove', prev: [state.originalKey, state.originalValue] } as DiffEntry;
			diffMap.set(state.originalKey, m.length);
			return m.concat(de);
		} else if (state.deleted) {
			// deleted entries with no original key/val don't go in the diff
			return m;
		}

		if (hasChanges && state.originalKey === undefined && state.originalValue === undefined) {
			// check to see if a remove operation for this entry is already in the diff
			const ode = diffMap.has(key) ? m[diffMap.get(key) as number] : undefined;
			if (ode && ode.op === 'remove' && ode.prev[0] === key && ode.prev[1] === value) {
				ode.op = 'keep';
				return m;
			}

			// append add op to diff for entry
			const de = { op: 'add', next: [key, value] } as DiffEntry;
			diffMap.set(key, m.length);
			return m.concat(de);
		} else if (!hasChanges) {
			// check to see if a remove operation for this entry is already in the diff
			const ode = diffMap.has(key) ? m[diffMap.get(key) as number] : undefined;
			if (ode && ode.op === 'remove' && ode.prev[0] === key && ode.prev[1] === value) {
				ode.op = 'keep';
				return m;
			}

			// append keep op to diff for entry
			const de = { op: 'keep', prev: [key, value] } as DiffEntry;
			diffMap.set(key, m.length);
			return m.concat(de);
		}

		// check to see if a remove operation for this entry is already in the diff
		const ode = diffMap.has(state.originalKey || '') ? m[diffMap.get(state.originalKey || '') as number] : undefined;
		if (ode && ode.op === 'remove' && ode.prev[0] === state.originalKey && ode.prev[1] === state.originalValue) {
			ode.op = 'replace';
			ode.next = [key, value];
			return m;
		}

		// append replace op to diff for entry
		const de = {
			op: 'replace',
			prev: [state.originalKey, state.originalValue],
			next: [key, value]
		} as DiffEntry;
		diffMap.set(state.originalKey as string, m.length);
		return m.concat(de);
	}, [] as Diff);
}

export function rebaseData(data: Data, base: [string, string][]): Data {
	const baseMap = new Map<string, string>(base);
	const processedKeys = new Set<string>();
	const nextData = Object.assign({}, data, {
		conflicts: [],
		_indicesMap: new Map(data._indicesMap),
		_indices: new Set(data._indices),
		_changedIndices: new Set<number>()
	});
	nextData._entries = data._entries
		.map((entry: Entry | undefined, index: number) => {
			if (entry === undefined) return undefined;
			const [key, value, state] = entry;
			const hasOriginalKeyVal = key === state.originalKey && value === state.originalValue;
			const hasChanges = !hasOriginalKeyVal || state.deleted;

			if (!hasChanges && baseMap.has(key) && baseMap.get(key) !== value) {
				// entry has new value in base and was not changed in data, so take the
				// new value
				processedKeys.add(key);
				const nextValue = baseMap.get(key);
				return [key, nextValue, Object.assign({}, state, { originalKey: key, originalValue: nextValue })] as Entry;
			}

			if (!hasChanges && !baseMap.has(key)) {
				// entry doesn't exists in base and was not changed, so mark it as
				// deleted
				processedKeys.add(key);
				nextData._deletedLength++;
				nextData.length--;
				return [key, value, Object.assign({}, state, { deleted: true })] as Entry;
			}

			if (hasChanges && state.originalKey && baseMap.has(state.originalKey)) {
				const baseValue = baseMap.get(state.originalKey);
				if (key !== state.originalKey && baseMap.has(key)) {
					// entry exists in base and was changed to match another entry in base
					const baseValue2 = baseMap.get(key);

					if (value === baseValue2) {
						// values are the same, so take the base key/val as the original
						processedKeys.add(key);
						processedKeys.add(state.originalKey);
						nextData._changedIndices.add(index);
						return [key, value, Object.assign({}, state, { originalKey: key, originalValue: baseValue2 })] as Entry;
					} else {
						// values are conflicting, so take the base key/val as the original
						// and add to conflicts
						const conflict = [
							{ op: 'add', next: [key, baseValue2] } as DiffEntry,
							{ op: 'replace', prev: [state.originalKey, state.originalValue], next: [key, value] } as DiffEntry
						] as [DiffEntry, DiffEntry];
						nextData.conflicts.push(conflict);
						processedKeys.add(key);
						processedKeys.add(state.originalKey);
						nextData._changedIndices.add(index);
						return [
							key,
							value,
							Object.assign({}, state, {
								originalKey: key,
								originalValue: baseValue2,
								rebaseConflict: conflict,
								rebaseConflictIndex: nextData.conflicts.length - 1
							})
						] as Entry;
					}
				}

				if (baseValue === state.originalValue) {
					// entry exists in base and was changed only in data, so take the new
					// base value as the originalValue
					processedKeys.add(state.originalKey);
					if (hasChanges) {
						nextData._changedIndices.add(index);
					}
					return [key, value, Object.assign({}, state, { originalValue: baseValue })] as Entry;
				} else if (state.deleted) {
					// entry was changed in base and was deleted in data, so take the new
					// base value as the original and add to conflicts
					const conflict = [
						{ op: 'replace', prev: [state.originalKey, state.originalValue], next: [key, baseValue] } as DiffEntry,
						{ op: 'remove', prev: [state.originalKey, state.originalValue] } as DiffEntry
					] as [DiffEntry, DiffEntry];
					nextData.conflicts.push(conflict);
					processedKeys.add(state.originalKey);
					nextData._changedIndices.add(index);
					return [
						key,
						value,
						Object.assign({}, state, {
							originalValue: baseValue,
							rebaseConflict: conflict,
							rebaseConflictIndex: nextData.conflicts.length - 1
						})
					] as Entry;
				}
			}

			if (hasChanges && baseMap.get(key) === value) {
				// changes to entry align with base key/val, so take the new value as
				// the originalValue
				processedKeys.add(key);
				return [key, value, Object.assign({}, state, { originalKey: key, originalValue: value })] as Entry;
			}

			if (hasChanges && baseMap.has(key)) {
				// changes to entry conflict with base key/val, so take the base value
				// as the original and add to conflicts
				const baseValue = baseMap.get(key) as string;
				processedKeys.add(key);
				nextData._changedIndices.add(index);
				const conflict = [
					{ op: 'add', next: [key, baseValue] } as DiffEntry,
					(state.originalKey === undefined
						? { op: 'add', next: [key, value] }
						: { op: 'replace', prev: [state.originalKey, state.originalValue || ''], next: [key, value] }) as DiffEntry
				] as [DiffEntry, DiffEntry];
				nextData.conflicts.push(conflict);
				return [
					key,
					value,
					Object.assign({}, state, {
						originalKey: key,
						originalValue: baseValue,
						rebaseConflict: conflict,
						rebaseConflictIndex: nextData.conflicts.length - 1
					})
				] as Entry;
			}

			if (!baseMap.has(key)) {
				// entry is not in base
				if (!state.deleted) {
					nextData._changedIndices.add(index);
				}
				processedKeys.add(key);
				const nextState = Object.assign({}, state);
				delete nextState.originalKey;
				delete nextState.originalValue;
				return [key, value, nextState] as Entry;
			}

			// entry is completely unchanged, so keep it's current state
			processedKeys.add(key);
			return [key, value, state] as Entry;
		})
		.concat(
			base.reduce((m: Entry[], [key, value]: [string, string]) => {
				// append new entries from base
				if (processedKeys.has(key)) return m;
				nextData.length++;
				const index = m.length + nextData._entries.length;
				nextData._indices.add(index);
				nextData._indicesMap.set(key, index);
				m.push([key, value, { originalKey: key, originalValue: value }]);
				return m;
			}, [] as Entry[])
		);
	nextData.hasChanges = nextData._changedIndices.size > 0;
	return nextData;
}
