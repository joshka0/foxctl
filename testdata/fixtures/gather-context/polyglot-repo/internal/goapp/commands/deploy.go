package commands

func RegisterDeployCommand(registry *Registry) {
	registry.Add("deploy", RunDeployCommand)
}

func RunDeployCommand() error {
	return nil
}
