package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dbdsl/internal/api"
)

func main() {
	var addr string
	var root string
	flag.StringVar(&addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&root, "root", "", "workspace root")
	flag.Parse()

	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("get cwd: %v", err)
		}
		root = filepath.Dir(cwd)
		if filepath.Base(cwd) != "dbdsl" {
			root = cwd
		}
	}

	server, err := api.New(api.Config{Root: root})
	if err != nil {
		log.Fatalf("create API server: %v", err)
	}

	fmt.Printf("DB Model Workbench API listening on http://%s\n", displayAddr(addr))
	fmt.Printf("Workspace root: %s\n", root)
	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}
