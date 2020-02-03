import {
	buildData,
	hasKey as hasDataKey,
	Data,
	DataAction,
	DataActionType,
	dataReducer,
	getKeyAtIndex,
	getValueAtIndex,
	setKeyAtIndex,
	setValueAtIndex,
	removeEntryAtIndex,
	appendEntry
} from './Data';
import { StringValidator } from '../useStringValidation';
import isActionType from '../util/isActionType';
import parseKeyValuePairs from '../util/parseKeyValuePairs';

export enum ActionType {
	SET_DATA = 'KVEDITOR__SET_DATA',
	PASTE = 'KVEDITOR__PASTE',
	BLUR = 'KVEDITOR__BLUR',
	FOCUS = 'KVEDITOR__FOCUS',
	SET_NODE_AT_INDEX = 'KVEDITOR__SET_NODE_AT_INDEX',
	SET_SELECTION = 'KVEDITOR__SET_SELECTION',
	SUBMIT_DATA = 'KVEDITOR__SUBMIT_DATA',
	SET_SUGGESTIONS = 'KVEDITOR__SET_SUGGESTIONS',
	SET_KEY_INPUT_SUGGESTIONS = 'KVEDITOR__SET_KEY_INPUT_SUGGESTIONS'
}

interface SetDataAction {
	type: ActionType.SET_DATA;
	data: Data;
}

interface PasteAction {
	type: ActionType.PASTE;
	text: string;
	selection: InputSelection;
	entryIndex: number;
	entryInnerIndex: 0 | 1;
}

interface BlurAction {
	type: ActionType.BLUR;
	entryIndex: number;
	entryInnerIndex: 0 | 1;
}

interface FocusAction {
	type: ActionType.FOCUS;
	entryIndex: number;
	entryInnerIndex: 0 | 1;
}

interface SetNodeAtIndexAction {
	type: ActionType.SET_NODE_AT_INDEX;
	ref: HTMLInputElement | HTMLTextAreaElement | null;
	entryIndex: number;
	entryInnerIndex: 0 | 1;
}

interface SetSelectionAction {
	type: ActionType.SET_SELECTION;
	selection: RowSelection;
}

interface SubmitDataAction {
	type: ActionType.SUBMIT_DATA;
	data: Data;
}

interface SetSuggestionsAction {
	type: ActionType.SET_SUGGESTIONS;
	suggestions: Suggestion[];
}

interface SetKeyInputSuggestionsAction {
	type: ActionType.SET_KEY_INPUT_SUGGESTIONS;
	suggestions: string[];
}

export type Action =
	| SetDataAction
	| PasteAction
	| BlurAction
	| FocusAction
	| SetNodeAtIndexAction
	| SetSelectionAction
	| SubmitDataAction
	| SetSuggestionsAction
	| SetKeyInputSuggestionsAction
	| DataAction;

export function isEditorAction(action: any): action is Action | DataAction {
	if (isActionType<Action>(ActionType, action)) {
		return true;
	}
	if (isActionType<DataAction>(DataActionType, action)) {
		return true;
	}
	return false;
}

export type Dispatcher = (actions: Action | Action[]) => void;

interface InputsState {
	currentSelection: RowSelection | null;
	prevSelection: RowSelection | null;
	refs: [HTMLInputElement | HTMLTextAreaElement | null, HTMLInputElement | HTMLTextAreaElement | null][];
}

export interface State {
	inputs: InputsState;
	selectedSuggestions: Map<number, Suggestion>;
	keyInputSuggestions: string[];
	suggestions: Suggestion[];
	data: Data;
	hasConflicts: boolean;
}

export interface StateProps {
	suggestions?: Suggestion[];
}

export function buildState(prevState: State, nextState: State): State {
	(() => {
		const { data } = nextState;
		if (data === prevState.data) return;

		nextState.hasConflicts = (data.conflicts || []).length > 0;
	})();

	(() => {
		const { suggestions, data, keyInputSuggestions } = nextState;
		if (suggestions === prevState.suggestions && data === prevState.data) return;

		const nextKeyInputSuggestions = suggestions.reduce((m: string[], s: Suggestion) => {
			if (hasDataKey(data, s.key)) return m;
			return m.concat(s.key);
		}, [] as string[]);
		if (
			nextKeyInputSuggestions.find((s: string, index: number) => {
				return keyInputSuggestions[index] !== s;
			})
		) {
			nextState.keyInputSuggestions = nextKeyInputSuggestions;
		}
	})();

	if (nextState === prevState) return prevState;
	return nextState;
}

export function initialState({ suggestions = [] }: StateProps): State {
	return buildState({} as State, {
		inputs: {
			currentSelection: null,
			prevSelection: null,
			refs: []
		},
		suggestions,
		selectedSuggestions: new Map(),
		keyInputSuggestions: [],
		data: buildData([]),
		hasConflicts: false
	});
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_DATA:
				nextState.data = action.data;
				return nextState;

			case ActionType.PASTE:
				let nextData = prevState.data;

				// Detect key=value paste
				if (action.text.match(/^(\S+=[^=]+\n?)+$/)) {
					for (const [key, val] of parseKeyValuePairs(action.text.trim())) {
						nextData = appendEntry(nextData, key, val);
					}
				} else if (action.entryInnerIndex === 1 && action.text.indexOf('\n') >= 0) {
					// make sure input expands into textarea
					const ref = (nextState.inputs.refs[action.entryIndex] || [])[action.entryInnerIndex];
					if (ref) {
						ref.blur();
					}
					nextState.inputs.currentSelection = {
						entryIndex: action.entryIndex,
						entryInnerIndex: action.entryInnerIndex,
						selectionStart: action.text.length,
						selectionEnd: action.text.length,
						direction: 'forward'
					};

					nextData = setValueAtIndex(nextData, action.text.replace(/\\n/g, '\n'), action.entryIndex);
				} else {
					// default paste behaviour

					let prevValue: string;
					if (action.entryInnerIndex === 0) {
						prevValue = getKeyAtIndex(nextData, action.entryIndex) || '';
					} else {
						prevValue = getValueAtIndex(nextData, action.entryIndex) || '';
					}

					const nextValue =
						prevValue.substring(0, action.selection.selectionStart) +
						action.text +
						prevValue.substring(action.selection.selectionEnd, prevValue.length);

					if (action.selection.selectionStart === action.selection.selectionEnd) {
						nextState.inputs.currentSelection = {
							entryIndex: action.entryIndex,
							entryInnerIndex: action.entryInnerIndex,
							selectionStart: action.selection.selectionStart + action.text.length,
							selectionEnd: action.selection.selectionStart + action.text.length,
							direction: action.selection.direction
						};
					} else {
						nextState.inputs.currentSelection = {
							entryIndex: action.entryIndex,
							entryInnerIndex: action.entryInnerIndex,
							selectionStart: action.selection.selectionStart,
							selectionEnd: action.selection.selectionStart + action.text.length,
							direction: action.selection.direction
						};
					}

					if (action.entryInnerIndex === 0) {
						nextData = setKeyAtIndex(nextData, nextValue, action.entryIndex);
					} else {
						nextData = setValueAtIndex(nextData, nextValue, action.entryIndex);
					}
				}

				nextState.data = nextData;
				return nextState;

			case ActionType.BLUR:
				// the following doesn't require a render to be triggered
				if (
					prevState.inputs.currentSelection &&
					prevState.inputs.currentSelection.entryIndex === action.entryIndex &&
					prevState.inputs.currentSelection.entryInnerIndex === action.entryInnerIndex
				) {
					prevState.inputs.prevSelection = prevState.inputs.currentSelection;
					prevState.inputs.currentSelection = null;
				}
				return prevState;

			case ActionType.FOCUS:
				return (() => {
					let hasChanges = false;
					const { prevSelection } = prevState.inputs;
					if (
						prevSelection &&
						getKeyAtIndex(prevState.data, prevSelection.entryIndex) === '' &&
						action.entryIndex !== prevSelection.entryIndex
					) {
						// remove row when key is deleted/empty and focus has moved to another row
						nextState.data = removeEntryAtIndex(prevState.data, prevSelection.entryIndex);
						hasChanges = true;
					}

					// the following doesn't require render to be triggered
					let state = hasChanges ? nextState : prevState;
					let ref = (state.inputs.refs[action.entryIndex] || [null, null])[action.entryInnerIndex];
					if (ref) {
						state.inputs.currentSelection = {
							selectionStart: ref.selectionStart || 0,
							selectionEnd: ref.selectionEnd || 0,
							direction: (ref.selectionDirection || 'forward') as RowSelection['direction'],
							entryIndex: action.entryIndex,
							entryInnerIndex: action.entryInnerIndex
						};
					}

					return hasChanges ? nextState : prevState;
				})();

			case ActionType.SET_NODE_AT_INDEX:
				let entryRefs = prevState.inputs.refs[action.entryIndex] || [null, null];
				if (action.entryInnerIndex === 0) {
					entryRefs = [action.ref as HTMLInputElement | HTMLTextAreaElement | null, entryRefs[1]];
				} else {
					entryRefs = [entryRefs[0], action.ref as HTMLInputElement | HTMLTextAreaElement | null];
				}
				prevState.inputs.refs[action.entryIndex] = entryRefs;
				return prevState; // don't trigger render

			case ActionType.SET_SELECTION:
				prevState.inputs.currentSelection = action.selection;
				return prevState; // don't trigger render

			case ActionType.SUBMIT_DATA:
				return (() => {
					let data = prevState.data;
					for (let [entry, entryIndex] of prevState.data) {
						// remove any entries with an empty key
						const [k, , s] = entry;
						if (k.trim() === '' && !s.deleted && entryIndex !== undefined) {
							data = removeEntryAtIndex(data, entryIndex);
						}
					}
					if (data !== prevState.data) {
						nextState.data = data;
						return nextState;
					}
					return prevState;
				})();

			case ActionType.SET_SUGGESTIONS:
				nextState.suggestions = action.suggestions;
				return nextState;

			case ActionType.SET_KEY_INPUT_SUGGESTIONS:
				nextState.keyInputSuggestions = action.suggestions;
				return nextState;

			case DataActionType.SET_KEY_AT_INDEX:
				const dataActions = [] as DataAction[];
				dataActions.push(action);

				const s = nextState.suggestions.find((s) => s.key === action.key);
				if (s) {
					dataActions.push({
						type: DataActionType.SET_VAL_AT_INDEX,
						value: s.valueTemplate.value,
						index: action.index
					});
					const { selectionStart, selectionEnd, direction } = s.valueTemplate;
					const valueInput = (nextState.inputs.refs[action.index] || [])[1];
					if (valueInput) {
						valueInput.value = s.valueTemplate.value;
						nextState.inputs.currentSelection = {
							entryIndex: action.index,
							entryInnerIndex: 1,
							selectionStart,
							selectionEnd,
							direction
						};
					}
					nextState.selectedSuggestions.set(action.index, s);
				} else {
					nextState.selectedSuggestions.delete(action.index);
				}

				nextState.data = dataReducer(prevState.data, dataActions);
				return nextState;

			default:
				if (isActionType<DataAction>(DataActionType, action)) {
					nextState.data = dataReducer(prevState.data, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	return buildState(prevState, nextState);
}

export interface SuggestionValueTemplate extends InputSelection {
	value: string;
}

export interface Suggestion {
	key: string;
	validateValue: StringValidator;
	valueTemplate: SuggestionValueTemplate;
}

export interface InputSelection {
	selectionStart: number;
	selectionEnd: number;
	direction: 'forward' | 'backward' | 'none';
}

export interface RowSelection extends InputSelection {
	entryIndex: number;
	entryInnerIndex: 0 | 1; // key | val
}
