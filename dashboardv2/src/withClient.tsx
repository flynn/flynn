import * as React from 'react';
import { Omit } from 'grommet/utils';
import { default as client, Client } from './client';

export const ClientContext = React.createContext(client);

export interface ClientProps {
	client: Client;
}

export default function withClient<P extends ClientProps>(Component: React.ComponentType<P>) {
	return function ClientComponent(props: Omit<P, keyof ClientProps>) {
		return (
			<ClientContext.Consumer>{(client) => <Component {...(props as P)} client={client} />}</ClientContext.Consumer>
		);
	};
}
