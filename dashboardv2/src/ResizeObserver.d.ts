interface Window {
	ResizeObserver: ResizeObserver;
}

/**
 * The ResizeObserver interface is used to observe changes to Element's content
 * rect.
 *
 * It is modeled after MutationObserver and IntersectionObserver.
 */
interface ResizeObserver {
	new (callback: ResizeObserverCallback);

	/**
	 * Adds target to the list of observed elements.
	 */
	observe: (target: Element) => void;

	/**
	 * Removes target from the list of observed elements.
	 */
	unobserve: (target: Element) => void;

	/**
	 * Clears both the observationTargets and activeTargets lists.
	 */
	disconnect: () => void;
}

/**
 * This callback delivers ResizeObserver's notifications. It is invoked by a
 * broadcast active observations algorithm.
 */
interface ResizeObserverCallback {
	(entries: ResizeObserverEntry[], observer: ResizeObserver): void;
}

interface ResizeObserverEntry {
	/**
	 * @param target The Element whose size has changed.
	 */
	new (target: Element);

	/**
	 * The Element whose size has changed.
	 */
	readonly target: Element;

	/**
	 * An object containing the new content box size of the observed element when the callback is run.
	 */
	readonly contentBoxSize: {
		// The length of the observed element's content box in the block dimension.
		// For boxes with a horizontal writing-mode, this is the vertical dimension, or
		// height; if the writing-mode is vertical, this is the horizontal dimension, or
		// width.
		readonly blockSize: number;

		// The length of the observed element's content box in the inline dimension.
		// For boxes with a horizontal writing-mode, this is the horizontal dimension, or
		// width; if the writing-mode is vertical, this is the vertical dimension, or
		// height.
		readonly inlineSize: number;
	};
}

interface DOMRectReadOnly {
	fromRect(other: DOMRectInit | undefined): DOMRectReadOnly;

	readonly x: number;
	readonly y: number;
	readonly width: number;
	readonly height: number;
	readonly top: number;
	readonly right: number;
	readonly bottom: number;
	readonly left: number;

	toJSON: () => any;
}
