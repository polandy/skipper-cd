package deploy

// loadVarsFile reads a KEY=VALUE env file and returns its entries ready to
// append to a process environment. Returns nil, nil when path is empty
// (vars_file not configured).
func loadVarsFile(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	return parseEnvFile(path)
}
