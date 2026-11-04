//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("Установщик HealCoreOps работает только в Windows. В Linux/macOS запускайте программу напрямую: ./healcoreops")
}
