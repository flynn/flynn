export interface PublicConfig {
	CONTROLLER_HOST: string;
	OAUTH_ISSUER: string;
	OAUTH_CLIENT_ID: string;
	OAUTH_CALLBACK_URI: string;

	[index: string]: string;
}

export interface OAuthCachedValues {
	codeVerifier: string;
	codeChallenge: string;
	redirectURI: string;
	state: string;
}

export interface OAuthCallbackResponse {
	state: string | null;
	code: string | null;
	error: string | null;
	error_description: string | null;
}

export interface OAuthToken {
	access_token: string;
	token_type: string;
	expires_in: number;
	refresh_token: string;
	refresh_token_expires_in: number;

	issued_time: number;
}

export enum MessageType {
	UNKNOWN = 'MessageType__UNKNOWN',
	CONFIG = 'MessageType__CONFIG',
	PING = 'MessageType__PING',
	PONG = 'MessageType__PONG',
	AUTH_REQUEST = 'MessageType__AUTH_REQUEST',
	AUTH_CALLBACK = 'MessageType__AUTH_CALLBACK',
	AUTH_TOKEN = 'MessageType__AUTH_TOKEN',
	ERROR = 'MessageType__ERROR'
}

export interface UnknownMessage {
	type: MessageType.UNKNOWN;
	payload: Message;
}

export interface ConfigMessage {
	type: MessageType.CONFIG;
	payload: PublicConfig;
}

export interface PingMessage {
	type: MessageType.PING;
}

export interface PongMessage {
	type: MessageType.PONG;
}

export interface AuthRequestMessage {
	type: MessageType.AUTH_REQUEST;
	payload: string;
}

export interface AuthCallbackMessage {
	type: MessageType.AUTH_CALLBACK;
	payload: string;
}

export interface AuthTokenMessage {
	type: MessageType.AUTH_TOKEN;
	payload: OAuthToken;
}

export interface ErrorMessage {
	type: MessageType.ERROR;
	payload: Error;
}

export type Message =
	| UnknownMessage
	| ConfigMessage
	| PingMessage
	| PongMessage
	| AuthRequestMessage
	| AuthCallbackMessage
	| AuthTokenMessage
	| ErrorMessage;
