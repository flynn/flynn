package main

func main() {
	ParseCommands(
		new(check),
		new(register),
		new(services),
		new(execCmd),
		new(expose),
	)
}
