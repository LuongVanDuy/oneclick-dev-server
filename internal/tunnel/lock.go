package tunnel

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

//go:embed versions.lock.json
var versionLockData []byte

type versionLock struct {
	Schema int          `json:"schema"`
	Binary binaryRecord `json:"cloudflared"`
}

type binaryRecord struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

var checksum = regexp.MustCompile(`^[a-f0-9]{64}$`)

func loadVersionLock() (versionLock, error) {
	var lock versionLock
	decoder := json.NewDecoder(strings.NewReader(string(versionLockData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lock); err != nil {
		return versionLock{}, err
	}
	if lock.Schema != 2 || lock.Binary.Version != "2026.8.2" ||
		lock.Binary.URL != "https://github.com/cloudflare/cloudflared/releases/download/2026.8.2/cloudflared-linux-amd64" ||
		!checksum.MatchString(lock.Binary.SHA256) {
		return versionLock{}, errors.New("cloudflared binary chưa được khóa URL, phiên bản và SHA-256")
	}
	return lock, nil
}
