import * as React from 'react';
import { Github as GithubIcon } from 'grommet-icons';
import { Heading, Accordion, AccordionPanel } from 'grommet';

import { isNotFoundError } from './client';
import {
	useAppWithDispatch,
	ActionType as AppActionType,
	Action as AppAction,
	State as AppState,
	initialState as initialAppState,
	reducer as appReducer
} from './useApp';
import isActionType from './util/isActionType';
import useRouter from './useRouter';
import { NavProtectionContext, buildNavProtectionContext } from './useNavProtection';

import { default as useErrorHandler } from './useErrorHandler';
import Notification from './Notification';
import Loading from './Loading';
import ExternalAnchor from './ExternalAnchor';
import { ActionType as FormationActionType, Action as FormationAction } from './FormationEditor';
const FormationEditor = React.lazy(() => import('./FormationEditor'));
const ReleaseHistory = React.lazy(() => import('./ReleaseHistory'));
const EnvEditor = React.lazy(() => import('./EnvEditor'));
const MetadataEditor = React.lazy(() => import('./MetadataEditor'));

export enum ActionType {}

export type Action = AppAction | FormationAction;

type Dispatcher = (actions: Action | Action[]) => void;

interface State {
	// useApp
	appState: AppState;

	// <FormationEditor>
	formationEditorError: Error | null;

	isAppDeleted: boolean;
	githubURL: string | null;
}

function initialState(props: Props): State {
	return {
		// useApp
		appState: initialAppState(),

		// <FormationEditor>
		formationEditorError: null,

		isAppDeleted: false,
		githubURL: null
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
			case FormationActionType.SET_ERROR:
				nextState.formationEditorError = action.error;
				return nextState;

			default:
				if (isActionType<AppAction>(AppActionType, action)) {
					nextState.appState = appReducer(prevState.appState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	(() => {
		const {
			appState: { app }
		} = nextState;
		nextState.isAppDeleted = !!(app && !!app.getDeleteTime());
	})();

	nextState.githubURL = (() => {
		const {
			appState: { app }
		} = nextState;
		if (!app) {
			return null;
		}
		return app.getLabelsMap().get('github.url') || null;
	})();

	return nextState;
}

export interface Props {
	name: string;
}

/*
 * <AppComponent> is a container displaying information and executing
 * operations on an App given it's name.
 *
 * Notibly it provides
 *	- viewing/editing process scale
 *	- viewing/deploying release and scale history
 *	- viewing/editing environment variables
 *	- viewing/editing app metadata
 *
 * Example:
 *
 *	<AppComponent name="apps/70f9e916-5612-4634-b6f1-2df75c1dd5de" />
 *
 */
export default function AppComponent(props: Props) {
	const { name } = props;
	const handleError = useErrorHandler();

	// Stream app
	const [
		{
			appState: { app, loading: appLoading, error: appError },
			isAppDeleted,
			githubURL,
			formationEditorError
		},
		dispatch
	] = React.useReducer(reducer, initialState(props));
	useAppWithDispatch(name, dispatch);

	React.useEffect(
		() => {
			if (appError) {
				// the error is intentionally not canceled
				if (app && isNotFoundError(appError)) {
					handleError(new Error(`"${app.getDisplayName()}" has been deleted!`));
					history.push('/' + location.search);
				} else {
					handleError(new Error(`${app ? app.getDisplayName() : 'App(' + name + ')'}: ${appError.message}`));
				}
			}

			let cancel = () => {};
			if (formationEditorError) {
				cancel = handleError(formationEditorError);
			}
			return cancel;
		},
		[appError, formationEditorError] // eslint-disable-line react-hooks/exhaustive-deps
	);
	React.useDebugValue(`App(${app ? name : 'null'})${appLoading ? ' (Loading)' : ''}`);

	const { history, location, urlParams } = useRouter();

	let panelIndex = 0;
	const panels = app
		? [
				<AppComponentPanel key="scale" label="Scale" index={panelIndex++} defaultActive={true}>
					<FormationEditor appName={app.getName()} dispatch={dispatch} />
				</AppComponentPanel>,

				<AppComponentPanel key="env" label="Environment Variables" index={panelIndex++} defaultActive={true}>
					<EnvEditor appName={app.getName()} />
				</AppComponentPanel>,

				<AppComponentPanel key="rs" label="Release History" index={panelIndex++} defaultActive={true}>
					<ReleaseHistory appName={app.getName()} />
				</AppComponentPanel>,

				<AppComponentPanel key="meta" label="Metadata" index={panelIndex++} defaultActive={false}>
					<MetadataEditor appName={app.getName()} />
				</AppComponentPanel>
		  ]
		: [];
	const metadataPanelIndex = panels.length - 1;
	const panelIndices = new Set(panels.map((p, i) => i));
	const metadataActive = new Set(urlParams.getAll('s').map((i: string) => parseInt(i, 10))).has(metadataPanelIndex);
	const activePanelIndices = new Set(panelIndices);
	if (!metadataActive) {
		activePanelIndices.delete(metadataPanelIndex);
	}
	urlParams.getAll('hs').forEach((i: string) => {
		const hiddenPanelIndex = parseInt(i, 10);
		activePanelIndices.delete(hiddenPanelIndex);
	});
	const handlePanelSectionChange = (indices: number[]) => {
		const nextActiveIndices = new Set(indices);
		const nextUrlParams = new URLSearchParams(urlParams);
		nextUrlParams.delete('s');
		nextUrlParams.delete('hs');
		const hiddenPanelIndices = new Set(panelIndices);
		hiddenPanelIndices.delete(metadataPanelIndex);
		panelIndices.forEach((i) => {
			if (i !== metadataPanelIndex && nextActiveIndices.has(i)) {
				hiddenPanelIndices.delete(i);
			}
		});
		if (nextActiveIndices.has(metadataPanelIndex)) {
			nextUrlParams.append('s', `${metadataPanelIndex}`);
		}
		Array.from(hiddenPanelIndices)
			.sort()
			.forEach((i: number) => nextUrlParams.append('hs', `${i}`));
		nextUrlParams.sort();
		history.replace(location.pathname + '?' + nextUrlParams.toString());
	};

	if (appLoading) {
		return <Loading />;
	}

	if (!app || isAppDeleted) {
		return null;
	}

	return (
		<>
			{app.getLabelsMap().get('flynn-system-app') === 'true' ? (
				<Notification message={'System apps are not fully supported.'} status="warning" margin="small" />
			) : null}
			<Heading margin="xsmall">
				<>
					{app.getDisplayName()}
					{githubURL ? (
						<>
							&nbsp;
							<ExternalAnchor href={githubURL}>
								<GithubIcon />
							</ExternalAnchor>
						</>
					) : null}
				</>
			</Heading>
			<Accordion
				multiple
				animate={false}
				onActive={handlePanelSectionChange}
				activeIndex={Array.from(activePanelIndices)}
			>
				{panels}
			</Accordion>
		</>
	);
}

interface AppComponentPanelProps {
	label: string;
	index: number;
	defaultActive: boolean;
	children: React.ReactNode;
}

const AppComponentPanel = ({ label, defaultActive, index, children }: AppComponentPanelProps) => {
	const navProtectionContext = React.useMemo(
		() => buildNavProtectionContext(defaultActive ? `hs=${index}` : `s=${index}`),
		[defaultActive, index]
	);
	return (
		<AccordionPanel label={label}>
			<React.Suspense fallback={<Loading />}>
				<NavProtectionContext.Provider value={navProtectionContext}>{children}</NavProtectionContext.Provider>
			</React.Suspense>
		</AccordionPanel>
	);
};
