//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("Display smoke runs only on Windows; cross-compile with GOOS=windows GOARCH=amd64.")
}
