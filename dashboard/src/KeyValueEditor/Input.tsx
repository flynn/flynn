import * as React from 'react';
import fz from 'fz';
import { StatusWarning as WarningIcon, Update as UpdateIcon } from 'grommet-icons';
import { Stack, Box, Button, TextArea } from 'grommet';
import { TextInput } from '../GrommetTextInput';
import useDebouncedInputOnChange from '../useDebouncedInputOnChange';
import { default as useStringValidation, StringValidator } from '../useStringValidation';
import { InputSelection } from './common';

export interface InputProps {
	placeholder: string;
	value: string;
	validateValue?: StringValidator;
	newValue?: string;
	hasConflict?: boolean;
	onChange: (value: string) => void;
	onBlur?: (e: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>) => void;
	onFocus?: (e: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>) => void;
	disabled?: boolean;
	suggestions?: string[];
	onSuggestionSelect?: (suggestion: string) => void;

	refHandler?: (ref: HTMLInputElement | HTMLTextAreaElement | null) => void;
	onPaste?: React.ClipboardEventHandler<HTMLInputElement | HTMLTextAreaElement>;
	onSelectionChange?: (selection: InputSelection) => void;
}

function Input(props: InputProps) {
	const [expanded, setExpanded] = React.useState(false);
	const multiline = React.useMemo<boolean>(() => props.value.indexOf('\n') >= 0, [props.value]);
	const textarea = React.useRef(null) as string & React.RefObject<HTMLTextAreaElement>;
	const [value, changeHandler, , cancelValue] = useDebouncedInputOnChange(props.value, props.onChange, 300);
	const validationErrorMsg = useStringValidation(value, props.validateValue || null);

	// focus textarea when expanded toggled to true
	React.useLayoutEffect(
		() => {
			if (expanded && textarea.current) {
				if (props.refHandler) {
					props.refHandler(textarea.current);
				}
				textarea.current.focus();
			}
		},
		[expanded] // eslint-disable-line react-hooks/exhaustive-deps
	);

	// remove input ref on unmount
	React.useLayoutEffect(
		() => {
			return () => {
				if (props.refHandler) {
					props.refHandler(null);
				}
			};
		},
		[] // eslint-disable-line react-hooks/exhaustive-deps
	);

	const filteredSuggestions =
		!props.suggestions || value === '' ? [] : props.suggestions.filter((s: string) => s !== value && fz(s, value));

	function selectionChangeHandler(e: React.SyntheticEvent<HTMLInputElement | HTMLTextAreaElement>) {
		const { selectionStart, selectionEnd, selectionDirection: direction } = e.target as
			| HTMLInputElement
			| HTMLTextAreaElement;
		if (props.onSelectionChange) {
			props.onSelectionChange({ selectionStart, selectionEnd, direction } as InputSelection);
		}
	}

	function suggestionSelectionHandler({ suggestion }: { [suggestion: string]: string }) {
		if (props.onSuggestionSelect) {
			cancelValue();
			props.onSuggestionSelect(suggestion);
		}
	}

	function renderInput() {
		const {
			placeholder,
			hasConflict,
			disabled,
			onChange,
			onBlur,
			onFocus,
			onSelectionChange,
			value: _value,
			refHandler,
			suggestions,
			onSuggestionSelect,
			...rest
		} = props;
		const inputRefProp = refHandler ? { ref: refHandler } : {};
		if (expanded) {
			return (
				<TextArea
					value={value}
					onChange={changeHandler}
					onInput={selectionChangeHandler}
					onSelect={selectionChangeHandler}
					onBlur={(e: React.SyntheticEvent<HTMLTextAreaElement>) => {
						expanded ? setExpanded(false) : void 0;
						if (onBlur) {
							onBlur(e);
						}
					}}
					onFocus={onFocus}
					resize="vertical"
					style={{ height: 500, paddingRight: hasConflict ? '2em' : undefined }}
					ref={textarea}
					{...rest}
				/>
			);
		}
		return (
			<TextInput
				type="text"
				style={hasConflict ? { paddingRight: '2em' } : undefined}
				disabled={disabled}
				placeholder={placeholder}
				value={value}
				onChange={changeHandler}
				onBlur={(e: React.SyntheticEvent<HTMLInputElement>) => {
					if (onBlur) {
						onBlur(e);
					}
				}}
				onInput={selectionChangeHandler}
				onSelect={selectionChangeHandler}
				suggestions={filteredSuggestions}
				onSuggestionSelect={suggestionSelectionHandler}
				onFocus={(e: React.SyntheticEvent<HTMLInputElement>) => {
					onFocus ? onFocus(e) : void 0;
					multiline ? setExpanded(true) : void 0;
				}}
				{...rest}
				{...inputRefProp}
			/>
		);
	}

	const { newValue, hasConflict } = props;
	if (newValue) {
		return (
			<Box fill direction="row">
				{renderInput()}
				<Button type="button" icon={<UpdateIcon />} onClick={() => props.onChange(newValue)} />
			</Box>
		);
	}
	if (hasConflict || validationErrorMsg !== null) {
		return (
			<Stack fill anchor="right" guidingChild="last" title={validationErrorMsg || ''}>
				<Box fill="vertical" justify="between" margin="xsmall">
					<WarningIcon />
				</Box>
				{renderInput()}
			</Stack>
		);
	}
	return renderInput();
}
const InputMemo = React.memo(Input);
export { InputMemo as Input };

(Input as any).whyDidYouRender = true;
