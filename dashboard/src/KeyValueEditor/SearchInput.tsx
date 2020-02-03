import * as React from 'react';
import { Search as SearchIcon } from 'grommet-icons';
import { Stack, Box, TextInput } from 'grommet';

import { Action } from './common';
import { DataActionType } from './Data';
import useDebouncedInputOnChange from '../useDebouncedInputOnChange';

interface Props {
	dispatch: (action: Action) => void;
}

function SearchInput({ dispatch }: Props) {
	const [_value, setValue] = React.useState('');
	const _onChange = React.useCallback(
		(value: string) => {
			setValue(value);
			dispatch({ type: DataActionType.APPLY_FILTER, query: value });
		},
		[dispatch]
	);
	const [value, onChange, flushValue] = useDebouncedInputOnChange(_value, _onChange);

	return (
		<Stack fill anchor="right" interactiveChild="last" guidingChild="last">
			<Box fill="vertical" justify="between" margin="xsmall">
				<SearchIcon />
			</Box>
			<TextInput
				type="search"
				title="Filter by key"
				placeholder="Type to filter by key"
				value={value}
				onChange={onChange}
				onBlur={flushValue}
				style={{ paddingRight: '32px' }}
			/>
		</Stack>
	);
}

const SearchInputMemo = React.memo(SearchInput);
export { SearchInputMemo as SearchInput };

(SearchInput as any).whyDidYouRender = true;
