package main

import (
	"fmt"

	"github.com/technosupport/ts-vms/internal/auth"
)

func main() {
	hash, _ := auth.HashPassword("adminpassword")
	fmt.Println(hash)
}
