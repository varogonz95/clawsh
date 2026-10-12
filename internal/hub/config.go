package hub

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/varogonz95/clawsh/internal/paths"
)

type Host struct {
	Name  string `toml:"name"`
	URL   string `toml:"url"`   // e.g. http://192.168.1.20:7420
	Token string `toml:"token"` // from `clawsh token` on that machine
}

type Config struct {
	Hosts []Host `toml:"host"`
}

func DefaultConfigPath() string {
	return filepath.Join(paths.ConfigDir(), "hosts.toml")
}

func LoadConfig(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("load %s: %w (see hosts.example.toml)", path, err)
	}
	seen := map[string]bool{}
	for i, h := range c.Hosts {
		if h.Name == "" || h.URL == "" {
			return nil, fmt.Errorf("%s: host #%d needs name and url", path, i+1)
		}
		if err := ValidateURL(h.URL); err != nil {
			return nil, fmt.Errorf("%s: host %q: %w", path, h.Name, err)
		}
		if seen[h.Name] {
			return nil, fmt.Errorf("%s: duplicate host name %q", path, h.Name)
		}
		seen[h.Name] = true
		c.Hosts[i].URL = strings.TrimRight(h.URL, "/")
	}
	return &c, nil
}

func (c *Config) Find(name string) (Host, error) {
	for _, h := range c.Hosts {
		if h.Name == name {
			return h, nil
		}
	}
	return Host{}, fmt.Errorf("unknown host %q", name)
}

// ValidateURL checks that a host URL is an http or https URL with a host.
// Its message never contains the URL, which may carry credentials.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("url must be http:// or https:// followed by a host")
	}
	return nil
}

// ValidateHost checks the fields hosts.toml requires of every host.
func ValidateHost(h Host) error {
	if h.Name == "" {
		return errors.New("name is required")
	}
	if h.URL == "" {
		return errors.New("url is required")
	}
	return ValidateURL(h.URL)
}

// EncodeConfig renders hosts as hosts.toml content. Comments and formatting
// of a hand-edited file are not kept.
func EncodeConfig(hosts []Host) ([]byte, error) {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(Config{Hosts: hosts}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
