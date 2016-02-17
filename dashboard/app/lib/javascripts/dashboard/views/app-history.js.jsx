import AppHistoryStore from 'dashboard/stores/app-history';
import Paginator from 'dashboard/paginator';
import Dispatcher from 'dashboard/dispatcher';
import { objectDiff } from 'dashboard/utils';
import ScrollPagination from 'ScrollPagination';
import ReleaseEvent from './release-event';
import ScaleEvent from './scale-event';

var Event = React.createClass({
	render: function () {
		var event = this.props.event;
		if (event.object_type === 'scale') {
			return this.renderScaleEvent(event);
		}
		if (event.object_type === 'app_release') {
			return this.renderReleaseEvent(event);
		}
		return (
			<div>
				Unsupported event type {event.object_type}
			</div>
		);
	},

	renderScaleEvent: function (event) {
		return (
			<ScaleEvent
				className="app-history-event"
				selectable={true}
				selected={this.props.selected}
				onSelect={this.props.onSelect}
				event={event}
				diff={event.diff}
				prevProcesses={event.data.prev_processes || {}}
				delta={event.delta}
				timestamp={event.created_at} />
		);
	},

	renderReleaseEvent: function (event) {
		return (
			<ReleaseEvent
				className="app-history-event"
				selectable={true}
				selected={this.props.selected}
				onSelect={this.props.onSelect}
				event={event}
				release={event.data.release}
				envDiff={event.envDiff}
				timestamp={event.created_at} />
		);
	},

	getInitialState: function () {
		return {
			showConfirmScaleModal: false
		};
	}
});

var AppHistory = React.createClass({
	render: function () {
		var state = this.state;
		var even = false;

		var selectedEvent = this.state.selectedEvent;
		var deployBtnDisabled = true;
		if (selectedEvent && selectedEvent.object_type === 'scale') {
			if (this.props.formation && objectDiff(this.props.formation.processes || {}, selectedEvent.data.processes || {}).length > 0) {
				deployBtnDisabled = false;
			}
		} else if (selectedEvent && selectedEvent.object_type === 'app_release') {
			if (this.props.release && this.props.release.id !== selectedEvent.object_id && (selectedEvent.data.release.artifacts || []).length) {
				deployBtnDisabled = false;
			}
		}

		return (
			<form className='app-history' onSubmit={function(e){e.preventDefault();}}>
				<header>
					<h2>App history</h2>
				</header>

				<section style={{position: 'relative', height: 300, overflowY: 'auto'}}>
					<ScrollPagination
						manager={this.props.scrollPaginationManager}
						hasPrevPage={this.state.hasPrevPage}
						hasNextPage={this.state.hasNextPage}
						unloadPage={this.__unloadPage}
						loadPrevPage={this.__loadPrevPage}
						loadNextPage={this.__loadNextPage}
						showNewItemsTop={true}>

						{state.pages.map(function (page) {
							return (
								<ScrollPagination.Page
									key={page.id}
									manager={this.props.scrollPaginationManager}
									id={page.id}
									component='ul'>

									{page.events.map(function (event) {
										var className = [];
										if (even) {
											className.push('even');
											even = false;
										} else {
											even = true;
										}
										var deployed = false;
										if (event.object_type === 'scale' && this.props.release &&
												this.props.formation && this.props.release.id === this.props.formation.release &&
												objectDiff(this.props.formation.processes || {}, event.data.processes || {}).length === 0) {
											deployed = true;
										} else if (event.object_type === 'app_release' && this.props.release && this.props.release.id === event.object_id) {
											deployed = true;
										}
										if (deployed) {
											className.push('deployed');
										}
										return (
											<li key={event.id} className={className.join(' ')}>
												<Event
													appID={this.props.appID}
													release={this.props.release}
													formation={this.props.formation}
													event={event}
													onSelect={this.__handleEventSelected}
													selected={this.state.selectedEvent && event.id === this.state.selectedEvent.id} />
											</li>
										);
									}, this)}
								</ScrollPagination.Page>
							);
						}, this)}

					</ScrollPagination>
				</section>

				<div className="deploy-btn-container">
					<button className="btn-green" disabled={deployBtnDisabled} onClick={this.__handleDeployBtnClick}>Deploy</button>
				</div>
			</form>
		);
	},

	getDefaultProps: function () {
		return {
			scrollPaginationManager: new ScrollPagination.Manager()
		};
	},

	getInitialState: function () {
		return {
			pages: [],
			selectedEvent: null
		};
	},

	componentDidMount: function () {
		var appID = this.props.appID;
		this.paginator = new Paginator({
			Store: AppHistoryStore,
			storeID: {appID: appID},
			fetchPrevPage: function () {
				Dispatcher.dispatch({
					name: 'FETCH_APP_HISTORY',
					direction: 'prev',
					appID: appID
				});
			},
			fetchNextPage: function () {
				Dispatcher.dispatch({
					name: 'FETCH_APP_HISTORY',
					direction: 'next',
					appID: appID
				});
			}
		});
		this.paginator.addChangeListener(this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		this.paginator.removeChangeListener(this.__handleStoreChange);
		this.paginator.close();
	},

	__handleStoreChange: function () {
		this.setState(this.paginator.getState());
	},

	__unloadPage: function (pageID) {
		this.paginator.unloadPage(pageID);
	},

	__loadPrevPage: function () {
		this.paginator.loadPrevPage();
	},

	__loadNextPage: function () {
		this.paginator.loadNextPage();
	},

	__handleEventSelected: function (event) {
		this.setState({
			selectedEvent: event
		});
	},

	__handleDeployBtnClick: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'CONFIRM_DEPLOY_APP_EVENT',
			appID: this.props.appID,
			eventID: this.state.selectedEvent.id
		});
	}
});

export default AppHistory;
