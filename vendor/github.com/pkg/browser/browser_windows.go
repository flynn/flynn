package browser

func openBrowser(url string) error {
	return runCmd("cmd", "/c", "start", url)
}
