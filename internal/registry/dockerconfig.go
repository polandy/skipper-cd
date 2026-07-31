package registry

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// dockerHubIndexKey is where docker login stores Docker Hub credentials —
// the legacy index URL, not the API host the client talks to.
const dockerHubIndexKey = "https://index.docker.io/v1/"

// DockerConfigPath returns the docker config.json path the docker CLI would
// use: $DOCKER_CONFIG/config.json when set, else ~/.docker/config.json.
func DockerConfigPath() string {
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return filepath.Join(dir, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".docker", "config.json")
}

// dockerAuth is one auths entry: either a base64 "user:pass" blob or explicit
// username/password fields.
type dockerAuth struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// DockerConfigCredentials resolves registry credentials from the docker
// config at path — the same credentials the host's pulls use, so a registry
// skipper can deploy from it can also ask about updates. The file is re-read
// per lookup (a docker login between checks takes effect without a restart);
// a missing or unparsable file means anonymous. Credential helpers
// (credsStore/credHelpers) are not consulted.
func DockerConfigCredentials(path string) Credentials {
	return func(host string) (string, string, bool) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", false
		}
		var cfg struct {
			Auths map[string]dockerAuth `json:"auths"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return "", "", false
		}
		for _, key := range credentialKeys(host) {
			if a, ok := cfg.Auths[key]; ok {
				return decodeAuth(a)
			}
		}
		return "", "", false
	}
}

// credentialKeys lists the auths keys that may hold a host's credentials, in
// precedence order: the host itself, its https:// form, and — for the Docker
// Hub API host — the legacy index key docker login writes.
func credentialKeys(host string) []string {
	keys := []string{host, "https://" + host}
	if host == dockerHubAPIHost {
		keys = append(keys, dockerHubIndexKey, "docker.io", "index.docker.io")
	}
	return keys
}

// decodeAuth extracts user/pass from one auths entry.
func decodeAuth(a dockerAuth) (string, string, bool) {
	if a.Auth != "" {
		decoded, err := base64.StdEncoding.DecodeString(a.Auth)
		if err != nil {
			return "", "", false
		}
		if user, pass, ok := strings.Cut(string(decoded), ":"); ok {
			return user, pass, true
		}
		return "", "", false
	}
	if a.Username != "" {
		return a.Username, a.Password, true
	}
	return "", "", false
}

// basicAuth renders the base64 payload of a Basic Authorization header.
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}
