import parseKeyValuePairs from './parseKeyValuePairs';

it('parses simple kv pairs', () => {
	const input = ['foo=bar', 'one=1', 'two=TWO', 'JIBBER=JABBER FOO'].join('\n');
	const output = Array.from(parseKeyValuePairs(input));
	expect(output).toEqual([
		['foo', 'bar'],
		['one', '1'],
		['two', 'TWO'],
		['JIBBER', 'JABBER FOO']
	]);
});

it('parses kv pairs with multi-line values', () => {
	const input = ['foo=bar', 'one=1', 'two=TWO', 'MULTI=foo\nbar\nbaz\nend', 'JIBBER=JABBER FOO'].join('\n');
	const output = Array.from(parseKeyValuePairs(input));
	expect(output).toEqual([
		['foo', 'bar'],
		['one', '1'],
		['two', 'TWO'],
		['MULTI', 'foo\nbar\nbaz\nend'],
		['JIBBER', 'JABBER FOO']
	]);
});

it('parses kv pairs with multi-line values where lines may end in equals sign(s)', () => {
	const input = ['foo=bar', 'one=1', 'two=TWO', 'MULTI=foo\nbar=\nbaz===\nend', 'JIBBER=JABBER FOO'].join('\n');
	const output = Array.from(parseKeyValuePairs(input));
	expect(output).toEqual([
		['foo', 'bar'],
		['one', '1'],
		['two', 'TWO'],
		['MULTI', 'foo\nbar=\nbaz===\nend'],
		['JIBBER', 'JABBER FOO']
	]);
});
