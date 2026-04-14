package obsidian

import "fmt"

// Client is the future transport-backed Obsidian adapter shell.
// Worker B/C/D can add read/search/write behavior without changing the config
// or interface contract established here.
type Client struct {
	cfg Config
}

// NewClient validates configuration and returns a client shell.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Transport == "" {
		defaults := DefaultConfig()
		if cfg.CLIPath == "" {
			cfg.CLIPath = defaults.CLIPath
		}
		if cfg.PostCreateDelay == 0 {
			cfg.PostCreateDelay = defaults.PostCreateDelay
		}
		cfg.Transport = defaults.Transport
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg}, nil
}

// Config returns the normalized client configuration.
func (c *Client) Config() Config {
	return c.cfg
}

// Target returns the normalized vault target.
func (c *Client) Target() VaultTarget {
	return c.cfg.Target
}

// Transport returns the active adapter transport.
func (c *Client) Transport() Transport {
	return c.cfg.Transport
}

// CommandSpec describes how CLI transport should be invoked for this client.
type CommandSpec struct {
	Binary string
	Vault  VaultTarget
}

// CLICommandSpec returns the normalized CLI invocation contract.
func (c *Client) CLICommandSpec() (CommandSpec, error) {
	if c.cfg.Transport != TransportCLI {
		return CommandSpec{}, fmt.Errorf("obsidian: client transport %q is not cli", c.cfg.Transport)
	}
	return CommandSpec{
		Binary: c.cfg.CLIPath,
		Vault:  c.cfg.Target,
	}, nil
}
