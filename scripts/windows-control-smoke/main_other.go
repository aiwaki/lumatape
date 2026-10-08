//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("Windows-only engine IPC smoke; default development host does not control a live UI.")
}
