import { assertEqual } from 'marbles/utils';
import GithubBranchesStore from '../stores/github-branches';
import GithubBranchesActions from '../actions/github-branches';

function getBranchesStoreId(props) {
	return {
		ownerLogin: props.ownerLogin,
		repoName: props.repoName
	};
}

function getState(props) {
	var state = {
		branchesStoreId: getBranchesStoreId(props)
	};

	var branchesState = GithubBranchesStore.getState(state.branchesStoreId);
	state.branchNames = branchesState.branchNames;

	return state;
}

var GithubBranchSelector = React.createClass({
	displayName: "Views.GithubBranchSelector",

	render: function () {
		var branchNames = this.state.branchNames;
		var defaultBranchName = this.props.defaultBranchName;
		var defaultBranchNameIndex;
		if (defaultBranchName) {
			defaultBranchNameIndex = branchNames.indexOf(defaultBranchName);
			if (defaultBranchNameIndex !== -1) {
				branchNames = [defaultBranchName].concat(branchNames.slice(0, defaultBranchNameIndex)).concat(branchNames.slice(defaultBranchNameIndex+1));
			}
		}
		var selectedBranchName = this.props.selectedBranchName;
		var deployedBranchName = this.props.deployedBranchName;
		var formatBranchName = this.__formatBranchName.bind(this, deployedBranchName);
		return (
			<div className="pretty-select">
				<select ref="branchSelector" value={selectedBranchName} onChange={this.__handleBranchChange}>
					{selectedBranchName && branchNames.indexOf(selectedBranchName) === -1 ? (
							<option value={selectedBranchName}>{formatBranchName(selectedBranchName)}</option>
					) : null}
					{branchNames.map(function (branch) {
						return (
							<option key={branch} value={branch}>{formatBranchName(branch)}</option>
						);
					}.bind(this))}
				</select>
			</div>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		GithubBranchesStore.addChangeListener(this.state.branchesStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (props) {
		var oldBranchesStoreId = this.state.branchesStoreId;
		var newBranchesStoreId = getBranchesStoreId(props);
		if ( !assertEqual(oldBranchesStoreId, newBranchesStoreId) ) {
			GithubBranchesStore.removeChangeListener(oldBranchesStoreId, this.__handleStoreChange);
			this.__handleStoreChange();
			GithubBranchesStore.addChangeListener(newBranchesStoreId, this.__handleStoreChange);
		}
	},

	componentWillUnmount: function () {
		GithubBranchesStore.removeChangeListener(this.state.branchesStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(getState(this.props));
	},

	__handleBranchChange: function () {
		var selectedBranchName = this.refs.branchSelector.getDOMNode().value;
		GithubBranchesActions.branchSelected(this.state.branchesStoreId, selectedBranchName);
	},

	__formatBranchName: function (deployedBranchName, branchName) {
		if (branchName === deployedBranchName) {
			return "*"+ branchName;
		}
		return branchName;
	}
});

export default GithubBranchSelector;
