package main

import (
	// ADK runner — wires the agent runner and plugin hooks.
	// internal/memory/ implements the Plugin interface callbacks (BeforeModel/AfterModel)
	// but does NOT import ADK types itself, keeping it testable without a full runner.
	_ "google.golang.org/adk/runner"
)

func main() {
	// TODO: parse configs/default.json into a config struct (internal/memory.Config)
	// TODO: construct MemoryPlugin with config and register with the ADK runner
	// TODO: start the ADK runner
}
