import * as React from 'react';
import { TextInput as GrommetTextInput } from 'grommet';
import ifDev from './ifDev';

// TextInput wraps grommet's TextInput so we can listen for `select` events on
// the input element. This behaviour is not currently supported by grommet due
// to an unfortunate naming collision with their suggestions feature.
// See https://github.com/grommet/grommet/issues/3118
export const TextInput = React.forwardRef(
	({ onSelect = () => {}, onSuggestionSelect = () => {}, ...rest }: any, _ref: any) => {
		const [dropTarget, setDropTarget] = React.useState<HTMLInputElement | null>(null);
		const ref = React.useMemo(
			() => {
				return { current: null as HTMLInputElement | null };
			},
			[] // eslint-disable-line react-hooks/exhaustive-deps
		);
		React.useEffect(
			() => {
				return () => {
					if (!ref.current) return;
					ref.current.removeEventListener('select', onSelect);
					ref.current = null;
				};
			},
			[] // eslint-disable-line react-hooks/exhaustive-deps
		);

		const [suggestionsOpen, setSuggestionsOpen] = React.useState(false);

		const handleSuggestionsOpen = React.useCallback(() => {
			setSuggestionsOpen(true);
		}, []);

		const handleSuggestionsClose = React.useCallback(() => {
			setSuggestionsOpen(false);
		}, []);

		const onKeyDown = React.useCallback(
			(event: React.KeyboardEvent) => {
				if (!ref.current) return;
				if (!suggestionsOpen) return;
				if (!(event.ctrlKey && (event.keyCode === 74 || event.keyCode === 75))) {
					// return unless ctr-j or ctr-k
					return;
				}
				// simulate ArrowUp for ctr-k and ArrowDown for ctr-j to control the
				// input suggestions
				event.preventDefault();
				const nextKeyCode = event.keyCode === 74 ? 40 : 38; // ctr-j is down, ctr-k is up
				const eventObj = document.createEvent('Events') as Event & { keyCode: number };
				eventObj.initEvent('keydown', true, true);
				eventObj.keyCode = nextKeyCode;
				ref.current.dispatchEvent(eventObj);
			},
			[ref, suggestionsOpen]
		);

		return (
			<GrommetTextInput
				onSuggestionsOpen={handleSuggestionsOpen}
				onSuggestionsClose={handleSuggestionsClose}
				onKeyDown={onKeyDown}
				onSelect={onSuggestionSelect}
				{...rest}
				dropTarget={dropTarget}
				ref={(input: any) => {
					if (typeof _ref === 'function') {
						_ref(input);
					}
					if (input) {
						ref.current = input;
						input.addEventListener('select', onSelect);
						setDropTarget(input);
					}
				}}
			/>
		);
	}
);

ifDev(() => ((GrommetTextInput as any).whyDidYouRender = true));
