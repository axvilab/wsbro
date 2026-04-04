package main

import (
	"log"

	"github.com/axvilab/wsbro/server"
)

func main() {
	srv := server.NewServer("6001")
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}
