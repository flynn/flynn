import * as React from 'react';
import { Anchor, AnchorProps } from 'grommet';
import { Omit } from 'grommet/utils';
import useRouter from './useRouter';

export interface Props extends Omit<AnchorProps, 'onClick'> {
	path: string;
	search?: string;
	onClick?: (e: React.MouseEvent) => void;
	onNav?: (path: string) => void;
	children: React.ReactNode;
}

export default function NavAnchor({ path, search, onClick, onNav, children, ...anchorProps }: Props) {
	const { history } = useRouter();

	return (
		<Anchor
			{...anchorProps}
			href={history.createHref({ pathname: path, search })}
			onClick={(e: React.MouseEvent) => {
				if (onClick) {
					onClick(e);
				}

				if (e.defaultPrevented) {
					// allow onClick prop to cancel this action via e.preventDefault()
					return;
				}

				if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) {
					// don't do anything if any modifier key is pressed
					// it's likely the user is trying to open the link in a new tab or window
					return;
				}

				e.preventDefault();

				// TODO(jvatic): Handle both path and search containing query params
				if (!history.push(path + search)) {
					return;
				}
				if (onNav) {
					onNav(path);
				}
			}}
		>
			{children}
		</Anchor>
	);
}
