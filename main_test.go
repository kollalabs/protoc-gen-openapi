package main

import (
	"log"
	"os"
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {

	// build and install protoc plugin before running tests
	out, err := exec.Command("go", "install").CombinedOutput()
	if err != nil {
		log.Println(string(out))
		log.Println(err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
