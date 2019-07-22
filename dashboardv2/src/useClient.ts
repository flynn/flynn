import * as React from 'react';
import { ClientContext } from './withClient';

export default function useClient() {
	return React.useContext(ClientContext);
}
