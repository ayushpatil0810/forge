package main

import (
	"fmt"
	"sample/pkg/api"
)

func main() {
	fmt.Println("Server starting...")
	api.HandleRequest()
}
