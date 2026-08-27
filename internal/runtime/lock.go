package runtimeenv

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

//go:embed versions.lock.json
var versionLockData []byte

type versionLock struct {
	Schema          int      `json:"schema"`
	Ubuntu          string   `json:"ubuntu"`
	RuntimeRevision string   `json:"runtimeRevision"`
	Packages        []string `json:"packages"`
}

var packageName = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]*$`)

func loadVersionLock() (versionLock, error) {
	var lock versionLock
	decoder := json.NewDecoder(strings.NewReader(string(versionLockData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lock); err != nil {
		return versionLock{}, fmt.Errorf("runtime version lock is invalid: %w", err)
	}
	if lock.Schema != 2 || lock.Ubuntu != "24.04" || !regexp.MustCompile(`^native-v[0-9]+$`).MatchString(lock.RuntimeRevision) {
		return versionLock{}, errors.New("runtime version lock has an unsupported schema or Ubuntu release")
	}
	required := []string{"nginx", "mariadb-server", "mariadb-client", "php8.3-fpm", "php8.3-mysql", "apparmor", "acl"}
	seen := map[string]bool{}
	for _, name := range lock.Packages {
		if !packageName.MatchString(name) || seen[name] {
			return versionLock{}, fmt.Errorf("runtime package %q is invalid or duplicated", name)
		}
		seen[name] = true
	}
	for _, name := range required {
		if !seen[name] {
			return versionLock{}, fmt.Errorf("runtime version lock is missing package %s", name)
		}
	}
	for _, name := range []string{"docker.io", "docker-compose-v2", "containerd"} {
		if seen[name] {
			return versionLock{}, fmt.Errorf("native runtime must not include %s", name)
		}
	}
	return lock, nil
}
