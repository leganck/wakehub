package main

import "testing"

func TestHelpAndVersionNoPanic(t *testing.T) {
	printVersion()
	printRootHelp()
	printServiceHelp()
}
