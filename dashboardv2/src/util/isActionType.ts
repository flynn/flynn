export default function isActionType<T>(dict: Object, action: any): action is T {
	if (!action && action.type) return false;
	if (Object.values(dict).includes(action.type)) {
		return true;
	}
	return false;
}
