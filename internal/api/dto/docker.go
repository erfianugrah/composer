package dto

// DockerExecInput is the request body for POST /api/v1/docker/exec.
//
// `command` is split on whitespace (see handler). Quoted arguments with embedded
// spaces are not supported today; for such cases prefer the dedicated endpoints
// (container start/stop/restart, stacks exec with service-level commands).
type DockerExecInput struct {
	Body struct {
		Command string `json:"command" minLength:"1" maxLength:"4096" doc:"Docker subcommand, e.g. 'ps -a', 'network ls', 'volume inspect my-vol'"`
	}
}

// DockerExecOutput is the generic response for docker/compose subcommands.
// ExitCode is always populated when the process ran (including non-zero exits);
// stdout/stderr contain the merged output.
type DockerExecOutput struct {
	Body struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
}
