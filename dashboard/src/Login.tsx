import * as React from 'react';

import { Box, Button, Form, FormField, TextInput } from 'grommet';

import useClient from './useClient';
import useWithCancel from './useWithCancel';
import useErrorHandler from './useErrorHandler';

export interface Props {
	onLoginSuccess: () => void;
}

export default function Login({ onLoginSuccess }: Props) {
	const [loginToken, setLoginToken] = React.useState('');
	const client = useClient();
	const withCancel = useWithCancel();
	const handleError = useErrorHandler();
	const handleFormSubmit = (e: React.SyntheticEvent) => {
		e.preventDefault();
		const cancel = client.login(loginToken, (s, err) => {
			if (err !== null) {
				const cancel = handleError(err);
				withCancel.set('login.error', cancel);
			} else {
				withCancel.call('login.error');
				onLoginSuccess();
			}
		});
		withCancel.set('login', cancel);
	};
	const handleTokenChange = (e: React.ChangeEvent<HTMLInputElement>) => {
		setLoginToken(e.target.value);
	};
	return (
		<Box basis="medium" align="center" justify="center" fill="vertical">
			<Form onSubmit={handleFormSubmit}>
				<FormField name="token" label="Login Token">
					<TextInput type="password" placeholder="token..." value={loginToken} onChange={handleTokenChange} />
				</FormField>
				<Button type="submit" primary disabled={loginToken.length === 0} label="Login" />
			</Form>
		</Box>
	);
}
