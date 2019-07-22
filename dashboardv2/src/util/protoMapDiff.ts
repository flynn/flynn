import * as jspb from 'google-protobuf';

export interface DiffOp<K, V> {
	key: K;
	op: 'add' | 'remove' | 'keep';
	value?: V;
}

export type Diff<K, V> = Array<DiffOp<K, V>>;

export type DiffConflict<K, V> = [DiffOp<K, V>, DiffOp<K, V>];

export enum DiffOption {
	INCLUDE_UNCHANGED,
	NO_DUPLICATE_KEYS
}

export default function protoMapDiff<K, V>(a: jspb.Map<K, V>, b: jspb.Map<K, V>, ...opts: DiffOption[]): Diff<K, V> {
	let _opts = new Set(opts);
	let diff: Diff<K, V> = [];
	a.forEach((av: V, ak: K) => {
		const bv = b.get(ak);
		if (bv === av) {
			if (_opts.has(DiffOption.INCLUDE_UNCHANGED)) {
				diff.push({
					op: 'keep',
					key: ak
				});
			}
			return;
		}
		if (bv === undefined) {
			diff.push({
				op: 'remove',
				key: ak
			});
			return;
		}
		if (!_opts.has(DiffOption.NO_DUPLICATE_KEYS)) {
			diff.push({ op: 'remove', key: ak });
		}
		diff.push({ op: 'add', key: ak, value: bv });
	});
	b.forEach((bv: V, bk: K) => {
		const av = a.get(bk);
		if (av === undefined) {
			diff.push({
				op: 'add',
				key: bk,
				value: bv
			});
			return;
		}
	});
	return diff;
}

export function applyProtoMapDiff<K, V>(m: jspb.Map<K, V>, diff: Diff<K, V>, mutate: boolean = false): jspb.Map<K, V> {
	let newMap = new jspb.Map<K, V>(m.toArray());
	if (mutate) {
		newMap = m;
	}
	diff.forEach((op: DiffOp<K, V>) => {
		switch (op.op) {
			case 'add':
				if (op.value) {
					newMap.set(op.key, op.value);
				}
				break;
			case 'remove':
				newMap.del(op.key);
				break;
			default:
				break;
		}
	});
	return newMap;
}

export function mergeProtoMapDiff<K, V>(a: Diff<K, V>, b: Diff<K, V>): [Diff<K, V>, DiffConflict<K, V>[], Set<K>] {
	const c = [] as Diff<K, V>;
	const conflicts = [] as DiffConflict<K, V>[];
	const conflictKeys = new Set<K>([]);

	function addOp(op: DiffOp<K, V>) {
		for (let i = 0; i < c.length; i++) {
			if (c[i].key === op.key && c[i].op === op.op) {
				if (c[i].value !== op.value) {
					conflicts.push([c[i], op]);
					conflictKeys.add(op.key);
				}
				c[i] = op;
				return;
			}
			if (c[i].key === op.key && c[i].op === 'add' && op.op === 'remove') {
				return;
			}
			if (c[i].key === op.key && c[i].op === 'remove' && op.op === 'add') {
				c[i] = op;
				return;
			}
		}
		c.push(op);
	}

	const alen = a.length;
	const blen = b.length;
	for (let i = 0, len = Math.max(alen, blen); i < len; i++) {
		if (i < alen) {
			addOp(a[i]);
		}
		if (i < blen) {
			addOp(b[i]);
		}
	}

	return [c, conflicts, conflictKeys];
}
