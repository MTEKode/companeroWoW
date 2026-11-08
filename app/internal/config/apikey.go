package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// ResolveKey returns the API key of the provider, looked up in this order: the literal
// APIKey, the OS environment variable named by APIKeyEnv, then the first .env file (see
// dotenvPaths) that defines APIKeyEnv. Errors name the variable but never the key value.
func (p APIProvider) ResolveKey() (string, error) {
	return resolveKey(p, dotenvPaths())
}

func resolveKey(p APIProvider, paths []string) (string, error) {
	if k := strings.TrimSpace(p.APIKey); k != "" {
		return k, nil
	}
	name := strings.TrimSpace(p.APIKeyEnv)
	if name == "" {
		return "", fmt.Errorf("falta la clave: el proveedor %q no define apiKeyEnv", p.Name)
	}
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v, nil
	}
	var problems []string
	for _, path := range paths {
		vars, err := loadDotenv(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				problems = append(problems, err.Error())
			}
			continue
		}
		if v := strings.TrimSpace(vars[name]); v != "" {
			return v, nil
		}
	}
	msg := fmt.Sprintf("falta la clave: define %s en el entorno o en .env", name)
	if len(problems) > 0 {
		msg += " (" + strings.Join(problems, "; ") + ")"
	}
	return "", errors.New(msg)
}
