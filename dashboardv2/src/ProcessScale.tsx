import * as React from 'react';
import styled from 'styled-components';
import { LinkUp as LinkUpIcon, LinkDown as LinkDownIcon } from 'grommet-icons';
import { Text, Box, BoxProps, Button, CheckBox } from 'grommet';
import useMergeDispatch from './useMergeDispatch';
import ifDev from './ifDev';

const valueCSS = (size: string) => `
	font-size: ${size === 'xsmall' ? '1em' : size === 'small' ? '2em' : '4em'};
	min-width: 1.2em;
	text-align: center;
	line-height: 1em;
`;

interface ValueInputProps {
	fontSize: string;
}

const ValueInput = styled.input`
	width: calc(0.7em + ${(props) => (props.value ? (props.value + '').length / 2 : 0)}em);
	border: none;
	&:focus {
		outline-width: 0;
	}
	font-weight: normal;
	${(props: ValueInputProps) => valueCSS(props.fontSize)};
`;

const ValueText = styled(Text)`
	${(props) => valueCSS(props.size as string)};
`;

const LabelText = styled(Text)`
	font-size: ${(props) => (props.size === 'xsmall' ? '0.75em' : props.size === 'small' ? '1em' : '1.5em')};
	line-height: 1.5em;
	margin: 0 0.5em;
`;

export enum ActionType {
	SET_VALUE = 'ProcessScale__SET_VALUE',
	SET_VALUE_EDITABLE = 'ProcessScale__SET_VALUE_EDITABLE',
	INCREMENT_VALUE = 'ProcessScale__INCREMENT_VALUE',
	DECREMENT_VALUE = 'ProcessScale__DECREMENT_VALUE',
	CONFIRM_SCALE_TO_ZERO = 'ProcessScale__CONFIRM_SCALE_TO_ZERO',
	UNCONFIRM_SCALE_TO_ZERO = 'ProcessScale__UNCONFIRM_SCALE_TO_ZERO'
}

interface SetValueAction {
	type: ActionType.SET_VALUE;
	value: number;
}

interface SetValueEditableAction {
	type: ActionType.SET_VALUE_EDITABLE;
	editable: boolean;
}

interface IncrementValueAction {
	type: ActionType.INCREMENT_VALUE;
}

interface DecrementValueAction {
	type: ActionType.DECREMENT_VALUE;
}

interface ConfirmScaleToZeroAction {
	type: ActionType.CONFIRM_SCALE_TO_ZERO;
}

interface UnconfirmScaleToZeroAction {
	type: ActionType.UNCONFIRM_SCALE_TO_ZERO;
}

export type Action =
	| SetValueAction
	| SetValueEditableAction
	| IncrementValueAction
	| DecrementValueAction
	| ConfirmScaleToZeroAction
	| UnconfirmScaleToZeroAction;

type Dispatcher = (actions: Action | Action[]) => void;

interface State {
	value: number;
	valueEditable: boolean;
}

function initialState(props: Props): State {
	return {
		value: props.value,
		valueEditable: false
	};
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_VALUE:
				nextState.value = action.value;
				return nextState;

			case ActionType.SET_VALUE_EDITABLE:
				nextState.valueEditable = action.editable;
				return nextState;

			case ActionType.INCREMENT_VALUE:
				nextState.value = prevState.value + 1;
				return nextState;

			case ActionType.DECREMENT_VALUE:
				nextState.value = prevState.value - 1;
				if (nextState.value < 0) return prevState;
				return nextState;

			case ActionType.CONFIRM_SCALE_TO_ZERO:
				return prevState;

			case ActionType.UNCONFIRM_SCALE_TO_ZERO:
				return prevState;

			default:
				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	return nextState;
}

export interface Props extends BoxProps {
	value: number;
	originalValue?: number;
	showDelta?: boolean;
	showLabelDelta?: boolean;
	label: string;
	size?: 'xsmall' | 'small' | 'large';
	mutable?: boolean;
	confirmScaleToZero?: boolean;
	scaleToZeroConfirmed?: boolean;
	dispatch: Dispatcher;
}

/*
 * <ProcessScale /> renders the amount a process is scaled to and allows
 * editing that amount when `editable=true`.
 *
 * Example:
 *	<ProcessScale value={3} label="web" />
 *
 * Example:
 *	<ProcessScale size="small" value={3} label="web" />
 *
 * Example:
 *	<ProcessScale value={3} label="web" mutable dispatch={dispatch} />
 *	See `ActionType` for list of action types.
 *	Caller is expected to implement `CONFIRM_SCALE_TO_ZERO` and `UNCONFIRM_SCALE_TO_ZERO`
 *  Watch `SET_VALUE`, `INCREMENT`, and `DECREMENT` to get changes in value
 */
const ProcessScale = React.memo(function ProcessScale({
	value: initialValue,
	originalValue = 0,
	showDelta = false,
	showLabelDelta = false,
	label,
	size = 'small',
	mutable = false,
	confirmScaleToZero = false,
	scaleToZeroConfirmed = false,
	dispatch: callerDispatch,
	...boxProps
}: Props) {
	const [{ value, valueEditable }, localDispatch] = React.useReducer(reducer, initialState(arguments[0]));
	const dispatch = useMergeDispatch(localDispatch, callerDispatch, false);

	const delta = React.useMemo(() => value - originalValue, [originalValue, value]);
	const deltaText = React.useMemo(() => {
		let sign = '+';
		if (delta < 0) {
			sign = '-';
		}
		return ` (${sign}${Math.abs(delta)})`;
	}, [delta]);

	// Handle incoming changes to props.value
	React.useEffect(() => {
		dispatch({ type: ActionType.SET_VALUE, value: initialValue });
	}, [initialValue, dispatch]);

	// Focus input when valueEditable enabled
	const valueInput = React.useRef(null) as React.RefObject<HTMLInputElement>;
	React.useLayoutEffect(() => {
		if (valueEditable && valueInput.current) {
			valueInput.current.focus();
		}
	}, [valueEditable, valueInput]);

	const handleIncrement = React.useCallback(
		(e: React.SyntheticEvent) => {
			e.preventDefault();
			dispatch({ type: ActionType.INCREMENT_VALUE });
		},
		[dispatch]
	);

	const handleDecrement = React.useCallback(
		(e: React.SyntheticEvent) => {
			e.preventDefault();
			dispatch({ type: ActionType.DECREMENT_VALUE });
		},
		[dispatch]
	);

	const handleChange = React.useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			dispatch({ type: ActionType.SET_VALUE, value: Math.max(parseInt(e.target.value, 10) || 0, 0) });
		},
		[dispatch]
	);

	const handleConfirmChange = React.useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			if (e.target.checked) {
				dispatch({ type: ActionType.CONFIRM_SCALE_TO_ZERO });
			} else {
				dispatch({ type: ActionType.UNCONFIRM_SCALE_TO_ZERO });
			}
		},
		[dispatch]
	);

	const handleValueTextClick = React.useCallback(() => {
		if (!mutable) return;
		dispatch({ type: ActionType.SET_VALUE_EDITABLE, editable: true });
	}, [mutable, dispatch]);

	const handleValueInputBlur = React.useCallback(() => {
		dispatch({ type: ActionType.SET_VALUE_EDITABLE, editable: false });
	}, [dispatch]);

	return (
		<Box
			align="center"
			border="all"
			round
			title={showDelta ? `Scaled ${delta > 0 ? 'up ' : delta < 0 ? 'down ' : ''}to ${value}${deltaText}` : ''}
			{...boxProps}
		>
			<Box
				direction="row"
				align="center"
				justify="center"
				border={boxProps.direction === 'row' ? 'right' : 'bottom'}
				fill="horizontal"
			>
				{valueEditable ? (
					<ValueInput
						ref={valueInput}
						fontSize={size}
						onBlur={handleValueInputBlur}
						onChange={handleChange}
						value={value}
					/>
				) : showDelta ? (
					<Box direction="row" justify="center" align="center">
						<Box justify="center">{delta > 0 ? <LinkUpIcon /> : delta < 0 ? <LinkDownIcon /> : null}</Box>
						<ValueText size={size} onClick={handleValueTextClick}>
							{value}
						</ValueText>
					</Box>
				) : (
					<ValueText size={size} onClick={handleValueTextClick}>
						{value}
					</ValueText>
				)}
				{mutable ? (
					<Box>
						<Button margin="xsmall" plain icon={<LinkUpIcon />} onClick={handleIncrement} />
						<Button margin="xsmall" plain icon={<LinkDownIcon />} onClick={handleDecrement} />
					</Box>
				) : null}
				{confirmScaleToZero && value === 0 && delta !== 0 ? (
					<Box margin={{ right: 'xsmall' }}>
						<CheckBox checked={scaleToZeroConfirmed} onChange={handleConfirmChange} />
					</Box>
				) : null}
			</Box>
			<Box flex="grow">
				<LabelText size={size}>{showLabelDelta ? `${label}${deltaText}` : label}</LabelText>
			</Box>
		</Box>
	);
});

export default ProcessScale;

ifDev(() => ((ProcessScale as any).whyDidYouRender = true));
