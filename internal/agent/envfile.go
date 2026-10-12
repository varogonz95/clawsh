package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseEnvFile reads KEY=VALUE lines. Blank lines and lines starting with #
// are skipped; only the first = splits, so values may contain =. Surrounding
// whitespace is trimmed from the key and the value.
func ParseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("%s:%d: want KEY=VALUE", path, n)
		}
		out[k] = strings.TrimSpace(v)
	}
	return out, sc.Err()
}

// ApplyEnvFile sets the file's variables in this process's environment, so
// sessions started afterwards inherit them. A missing file is an error only
// when required (the flag was given explicitly).
func ApplyEnvFile(path string, required bool) error {
	vars, err := ParseEnvFile(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return err
	}
	for k, v := range vars {
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return nil
}
