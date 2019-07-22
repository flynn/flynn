import State from './WindowedListState';

it('selects items within viewport to display', () => {
	const state = new State();
	state.length = 1000;
	state.viewportHeight = 400;
	state.defaultHeight = 100;
	state.calculateVisibleIndices();

	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);
});

it('responds to a change in scroll position', () => {
	const state = new State();
	state.length = 1000;
	state.viewportHeight = 400;
	state.defaultHeight = 100;

	state.updateScrollPosition(120);
	expect(state.visibleIndexTop).toEqual(1);
	expect(state.visibleLength).toEqual(4);

	state.updateScrollPosition(90);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);
});

it('responds to small incremental changes in scroll position', () => {
	const state = new State();
	state.length = 1000;
	state.viewportHeight = 400;
	state.defaultHeight = 100;

	state.updateScrollPosition(30);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);

	state.updateScrollPosition(60);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);

	state.updateScrollPosition(90);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);

	state.updateScrollPosition(110);
	expect(state.visibleIndexTop).toEqual(1);
	expect(state.visibleLength).toEqual(4);

	state.updateScrollPosition(99);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);
});

it('sets padding for top and bottom equal to height of items out of range', () => {
	const state = new State();
	state.length = 1000;
	state.viewportHeight = 400;
	state.defaultHeight = 100;

	state.calculateVisibleIndices(); // make sure padding is already calculated

	state.updateScrollPosition(220);
	expect(state.visibleIndexTop).toEqual(2);
	expect(state.visibleLength).toEqual(4);
	expect(state.paddingTop).toEqual(200);
	expect(state.paddingBottom).toEqual(99400);

	state.updateScrollPosition(200);
	expect(state.visibleIndexTop).toEqual(2);
	expect(state.visibleLength).toEqual(4);
	expect(state.paddingTop).toEqual(200);
	expect(state.paddingBottom).toEqual(99400);

	state.updateScrollPosition(100);
	expect(state.visibleIndexTop).toEqual(1);
	expect(state.visibleLength).toEqual(4);
	expect(state.paddingTop).toEqual(100);
	expect(state.paddingBottom).toEqual(99500);

	state.updateScrollPosition(10);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(99600);

	state.updateScrollPosition(0);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(99600);
});

it('responds to a change in item heights', () => {
	const state = new State();
	state.length = 1000;
	state.viewportHeight = 400;
	state.defaultHeight = 100;

	state.calculateVisibleIndices();

	state.updateHeightAtIndex(1, 250);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(3);

	state.updateHeightAtIndex(0, 250);
	state.calculateVisibleIndices();
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(2);
});

it('padding reflects actual item heights', () => {
	const state = new State();
	state.length = 100;
	state.viewportHeight = 400;
	state.defaultHeight = 100;

	state.calculateVisibleIndices();

	state.updateHeightAtIndex(0, 250);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(3);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(9700);

	state.updateHeightAtIndex(10, 250);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(3);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(9850);

	state.updateHeightAtIndex(11, 250);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(3);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(10000);

	state.updateHeightAtIndex(12, 250);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(3);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(10150);

	state.updateScrollPosition(400);
	expect(state.visibleIndexTop).toEqual(2);
	expect(state.visibleLength).toEqual(4);
	expect(state.paddingTop).toEqual(350);
	expect(state.paddingBottom).toEqual(9850);

	// push item into view
	state.updateHeightAtIndex(0, 301);
	expect(state.visibleIndexTop).toEqual(1);
	expect(state.visibleLength).toEqual(5);
	expect(state.paddingTop).toEqual(301);
	expect(state.paddingBottom).toEqual(9850);

	// reset
	state.updateScrollPosition(0);
	expect(state.visibleIndexTop).toEqual(0);
	state.updateHeightAtIndex(0, 100);
	state.updateHeightAtIndex(10, 100);
	state.updateHeightAtIndex(11, 100);
	state.updateHeightAtIndex(12, 100);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(4);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(9600);

	// pull item into view
	state.updateHeightAtIndex(3, 19);
	expect(state.visibleIndexTop).toEqual(0);
	expect(state.visibleLength).toEqual(5);
	expect(state.paddingTop).toEqual(0);
	expect(state.paddingBottom).toEqual(9500);
});
