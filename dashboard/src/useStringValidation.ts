import * as React from 'react';
import { debounce } from 'lodash';

export type StringValidator = (value: string) => null | string;

export default function useStringValidation(
	value: string,
	validator: StringValidator | null,
	timeout = 300
): string | null {
	const [validationErrorMsg, setValidationErrorMsg] = React.useState<string | null>(null);
	const doValidation = React.useMemo(
		() =>
			debounce((value: string) => {
				if (!validator) {
					setValidationErrorMsg(null);
					return;
				}
				const isValidOrErrorMsg = validator(value);
				setValidationErrorMsg(isValidOrErrorMsg);
			}, timeout),
		[validator, timeout]
	);

	// handle new value being passed in
	React.useEffect(
		() => {
			doValidation.cancel();
			doValidation(value);
		},
		[value] // eslint-disable-line react-hooks/exhaustive-deps
	);

	// make sure it doesn't fire after component unmounted
	React.useEffect(
		() => {
			return doValidation.cancel();
		},
		[] // eslint-disable-line react-hooks/exhaustive-deps
	);

	return validationErrorMsg;
}
