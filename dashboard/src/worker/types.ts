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

export interface ErrorWithID extends Error {
	id: string;
}

export enum MessageType {
	UNKNOWN = 'UNKNOWN',
	CONFIG = 'CONFIG',
	PING = 'PING',
	PONG = 'PONG',
	RETRY_AUTH = 'RETRY_AUTH',
	AUTH_REQUEST = 'AUTH_REQUEST',
	AUTH_CALLBACK = 'AUTH_CALLBACK',
	AUTH_TOKEN = 'AUTH_TOKEN',
	AUTH_ERROR = 'AUTH_ERROR',
	ERROR = 'ERROR',
	CLEAR_ERROR = 'CLEAR_ERROR'
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
	payload: Array<string>;
}

export interface RetryAuthMessage {
	type: MessageType.RETRY_AUTH;
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

export interface AuthErrorMessage {
	type: MessageType.AUTH_ERROR;
	payload: ErrorWithID;
}

export interface ErrorMessage {
	type: MessageType.ERROR;
	payload: ErrorWithID;
}

type ErrorID = string;
export interface ClearErrorMessage {
	type: MessageType.CLEAR_ERROR;
	payload: ErrorID;
}

export type Message =
	| UnknownMessage
	| ConfigMessage
	| PingMessage
	| PongMessage
	| RetryAuthMessage
	| AuthRequestMessage
	| AuthCallbackMessage
	| AuthTokenMessage
	| AuthErrorMessage
	| ErrorMessage
	| ClearErrorMessage;
