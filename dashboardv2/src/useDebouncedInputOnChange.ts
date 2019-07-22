import * as React from 'react';
import { debounce } from 'lodash';

export default function useDebouncedInputOnChange(
	value: string,
	onChange: (value: string) => void,
	timeout = 60
): [string, (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => void, () => void, () => void] {
	const [_value, setValue] = React.useState(value);
	const _onChange = React.useMemo(() => {
		return debounce((value: string) => {
			onChange(value);
		}, timeout);
	}, [onChange, timeout]);

	// handle new value being passed in
	React.useEffect(() => {
		_onChange.cancel();
		setValue(value);
	}, [_onChange, value]);

	// make sure it doesn't fire after component unmounted
	React.useEffect(() => {
		return _onChange.cancel();
	}, [_onChange]);

	return [
		_value,
		(e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			_onChange.cancel();
			const nextValue = e.target.value;
			setValue(nextValue);
			_onChange(nextValue);
		},
		() => {
			// flush
			_onChange.cancel();
			onChange(_value);
		},
		() => {
			// cancel
			_onChange.cancel();
		}
	];
}
