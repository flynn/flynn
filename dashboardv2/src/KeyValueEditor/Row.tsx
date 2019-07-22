import * as React from 'react';
import { Box } from 'grommet';

import { InputSelection, Suggestion, Dispatcher, ActionType } from './common';
import { Entry, DataActionType } from './Data';
import { Input } from './Input';

type EntryInnerIndex = 0 | 1;

export interface RowProps {
	index: number;
	entry: Entry;
	keyPlaceholder: string;
	valuePlaceholder: string;
	keyInputSuggestions: string[];
	selectedKeySuggestion: Suggestion | null;
	dispatch: Dispatcher;
}

function Row(props: RowProps) {
	const [key, value, { rebaseConflict, originalValue }] = props.entry;
	const hasConflict = rebaseConflict !== undefined;

	const { index, dispatch } = props;

	const isPasting = React.useMemo(() => ({ current: false }), []);

	const refHandler = React.useCallback(
		(entryIndex: number, entryInnerIndex: 0 | 1, ref: HTMLInputElement | HTMLTextAreaElement | null) => {
			dispatch({ type: ActionType.SET_NODE_AT_INDEX, entryIndex, entryInnerIndex, ref });
		},
		[dispatch]
	);

	const blurHandler = React.useCallback(
		(
			entryIndex: number,
			entryInnerIndex: 0 | 1,
			event: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>
		) => {
			dispatch({ type: ActionType.BLUR, entryIndex, entryInnerIndex });
		},
		[dispatch]
	);

	const focusHandler = React.useCallback(
		(
			entryIndex: number,
			entryInnerIndex: 0 | 1,
			event: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>
		) => {
			dispatch({ type: ActionType.FOCUS, entryIndex, entryInnerIndex });
		},
		[dispatch]
	);

	const selectionChangeHandler = React.useCallback(
		(entryIndex: number, entryInnerIndex: 0 | 1, selection: InputSelection) => {
			dispatch({
				type: ActionType.SET_SELECTION,
				selection: Object.assign({}, selection, { entryIndex, entryInnerIndex })
			});
		},
		[dispatch]
	);

	const pasteHandler = React.useCallback(
		(entryIndex: number, entryInnerIndex: 0 | 1, e: React.ClipboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			e.preventDefault();
			isPasting.current = true;
			const text = e.clipboardData.getData('text/plain');
			const input = e.target as HTMLInputElement | HTMLTextAreaElement;
			const selection = {
				selectionStart: input.selectionStart || 0,
				selectionEnd: input.selectionEnd || 0,
				direction: (input.selectionDirection || 'forward') as InputSelection['direction']
			};
			dispatch({ type: ActionType.PASTE, text, selection, entryIndex, entryInnerIndex });
			isPasting.current = false;
		},
		[dispatch, isPasting]
	);

	const keyInputRefHandler = React.useCallback(
		(ref: HTMLInputElement | HTMLTextAreaElement | null) => {
			refHandler(index, 0, ref);
		},
		[index, refHandler]
	);

	const keyChangeHandler = React.useCallback(
		(value: string) => {
			dispatch({ type: DataActionType.SET_KEY_AT_INDEX, key: value, index });
		},
		[index, dispatch]
	);

	const keyBlurHandler = React.useCallback(
		(e: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			blurHandler(index, 0, e);
		},
		[index, blurHandler]
	);

	const keyFocusHandler = React.useCallback(
		(e: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			focusHandler(index, 0, e);
		},
		[index, focusHandler]
	);

	const keySelectionChangeHandler = React.useCallback(
		(selection: InputSelection) => {
			selectionChangeHandler(index, 0, selection);
		},
		[index, selectionChangeHandler]
	);

	const keySuggestionSelectionHandler = React.useCallback(
		(suggestion: string) => {
			dispatch({ type: DataActionType.SET_KEY_AT_INDEX, key: suggestion, index });
		},
		[index, dispatch]
	);

	const keyPasteHandler = React.useCallback(
		(e: React.ClipboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			pasteHandler(index, 0, e);
		},
		[index, pasteHandler]
	);

	const valueInputRefHandler = React.useCallback(
		(ref: HTMLInputElement | HTMLTextAreaElement | null) => {
			refHandler(index, 1, ref);
		},
		[index, refHandler]
	);

	const valueChangeHandler = React.useCallback(
		(value: string) => {
			if (isPasting.current) return; // race condition workaround
			dispatch({ type: DataActionType.SET_VAL_AT_INDEX, value, index });
		},
		[index, dispatch, isPasting]
	);

	const valueBlurHandler = React.useCallback(
		(e: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			if (isPasting.current) return; // race condition workaround
			blurHandler(index, 1, e);
		},
		[index, blurHandler, isPasting]
	);

	const valueFocusHandler = React.useCallback(
		(e: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			focusHandler(index, 1, e);
		},
		[index, focusHandler]
	);

	const valueSelectionChangeHandler = React.useCallback(
		(selection: InputSelection) => {
			selectionChangeHandler(index, 1, selection);
		},
		[index, selectionChangeHandler]
	);

	const valuePasteHandler = React.useCallback(
		(e: React.ClipboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
			pasteHandler(index, 1, e);
		},
		[index, pasteHandler]
	);

	return (
		<>
			<Input
				refHandler={keyInputRefHandler}
				placeholder={props.keyPlaceholder}
				value={key}
				hasConflict={hasConflict}
				onChange={keyChangeHandler}
				onBlur={keyBlurHandler}
				onFocus={keyFocusHandler}
				onSelectionChange={keySelectionChangeHandler}
				suggestions={props.keyInputSuggestions}
				onSuggestionSelect={keySuggestionSelectionHandler}
				onPaste={keyPasteHandler}
			/>
			<Box flex="grow" justify="center">
				=
			</Box>
			<Input
				refHandler={valueInputRefHandler}
				placeholder={props.valuePlaceholder}
				value={value}
				validateValue={props.selectedKeySuggestion ? props.selectedKeySuggestion.validateValue : undefined}
				newValue={hasConflict ? originalValue : undefined}
				onChange={valueChangeHandler}
				onBlur={valueBlurHandler}
				onFocus={valueFocusHandler}
				onSelectionChange={valueSelectionChangeHandler}
				onPaste={valuePasteHandler}
			/>
		</>
	);
}
const RowMemo = React.memo(Row);
export { RowMemo as Row };

(Row as any).whyDidYouRender = true;
