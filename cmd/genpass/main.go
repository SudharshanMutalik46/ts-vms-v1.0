package main

import (
	"fmt"

	"github.com/technosupport/ts-vms/internal/auth"
)

func main() {
	hash, err := auth.HashPassword("password")
	if err != nil {
		panic(err)
	}
	fmt.Println(hash)
}
