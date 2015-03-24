package volume

// Required to make CLI compile on Windows
func IsMount(path string) (bool, error) {
	return false, nil
}
