//go:build !windows

package main

import "fmt"

func main() { fmt.Println("Windows-only developer UI helper; does not run on this OS") }
