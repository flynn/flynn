package browser

func openBrowser(url string) error {
	// try sensible-browser first
	if err := runCmd("sensible-browser", url); err == nil {
		return nil
	}
	// sensible-browser not availble, try xdg-open
	return runCmd("xdg-open", url)
}
