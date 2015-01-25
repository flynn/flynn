package main

func main() {
	ParseCommands(
		new(register),
		new(instances),
		new(execCmd),
	)
}
