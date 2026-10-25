// Command proxie starts the proxie HTTP server.
package main

import (
	"log"
	"os"

	"github.com/schmenniooo/proxie/internal/server"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}

	if err := server.New().Run(addr); err != nil {
		log.Fatal(err)
	}
}
