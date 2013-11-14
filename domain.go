package main

var cmdDomain = &Command{
	Run:   runDomain,
	Usage: "domain DOMAIN",
	Short: "adds a domain",
	Long:  "Adds a frontend domain to the app",
}

func runDomain(cmd *Command, args []string) {
	must(Put(&struct{}{}, "/apps/"+mustApp()+"/domains/"+args[0], struct{}{}))
}
