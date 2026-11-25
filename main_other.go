//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("native-win32 targets Windows only")
}

