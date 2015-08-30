package main

import (
	"fmt"
	"log"
)

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

func promptReplaceRemote(remote string) (bool, error) {
	remotes, err := gitRemoteNames()
	if err != nil {
		return false, err
	}
	for _, r := range remotes {
		if r == remote {
			fmt.Println("There is already a git remote called", remote)
			if !promptYesNo("Are you sure you want to replace it?") {
				log.Println("The remote was not created. Please declare the desired local git remote name with --remote flag.")
				return false, nil
			}
		}
	}
	return true, nil
}
