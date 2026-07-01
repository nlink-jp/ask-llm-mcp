package main

import "github.com/nlink-jp/ask-llm-mcp/cmd"

var version = "dev"

func main() {
	cmd.Execute(version)
}
