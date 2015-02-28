package main

import "fmt"

func promptYesNo(msg string) (result bool) {
	fmt.Print(msg)
	fmt.Print(" (yes/no): ")
	for {
		var answer string
		fmt.Scanln(&answer)
		switch answer {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Print("Please type 'yes' or 'no': ")
		}
	}
}
