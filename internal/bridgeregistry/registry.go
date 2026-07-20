package bridgeregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

const registryDirEnv = "PINCHTAB_BRIDGE_REGISTRY_DIR"

// Entry describes one standalone bridge attached to an external CDP target.
type Entry struct {
	PID       int       `json:"pid"`
	Port      string    `json:"port"`
	Target    string    `json:"target"`
	Cwd       string    `json:"cwd"`
	StartTime time.Time `json:"startTime"`
}

// LiveTargetError reports the existing bridge that owns a CDP target.
type LiveTargetError struct {
	Entry Entry
}

func (e *LiveTargetError) Error() string {
	return fmt.Sprintf("CDP target already attached by bridge pid %d on port %s", e.Entry.PID, e.Entry.Port)
}

// Register atomically claims target for a standalone bridge.
func Register(target, port string) (Entry, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Entry{}, fmt.Errorf("CDP target is required")
	}

	dir, err := registryDir()
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Entry{}, fmt.Errorf("create bridge registry: %w", err)
	}

	entry := Entry{PID: os.Getpid(), Port: strings.TrimSpace(port), Target: target, StartTime: time.Now().UTC()}
	entry.Cwd, _ = os.Getwd()
	path := entryPath(dir, target)
	for attempts := 0; attempts < 2; attempts++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			encodeErr := json.NewEncoder(f).Encode(entry)
			closeErr := f.Close()
			if encodeErr != nil {
				_ = os.Remove(path)
				return Entry{}, fmt.Errorf("write bridge registry: %w", encodeErr)
			}
			if closeErr != nil {
				_ = os.Remove(path)
				return Entry{}, fmt.Errorf("close bridge registry: %w", closeErr)
			}
			return entry, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return Entry{}, fmt.Errorf("claim bridge target: %w", err)
		}

		existing, readErr := readEntry(path)
		if readErr == nil && live(existing) {
			return Entry{}, &LiveTargetError{Entry: existing}
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return Entry{}, fmt.Errorf("remove stale bridge registry entry: %w", removeErr)
		}
	}
	return Entry{}, fmt.Errorf("could not claim CDP target")
}

// Remove releases an entry only when it is still owned by the supplied process.
func Remove(entry Entry) error {
	dir, err := registryDir()
	if err != nil {
		return err
	}
	path := entryPath(dir, entry.Target)
	current, err := readEntry(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.PID != entry.PID || current.StartTime != entry.StartTime {
		return nil
	}
	return os.Remove(path)
}

// List returns live entries and removes entries whose process has exited.
func List() []Entry {
	dir, err := registryDir()
	if err != nil {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil
	}
	entries := make([]Entry, 0, len(files))
	for _, path := range files {
		entry, err := readEntry(path)
		if err != nil || !live(entry) {
			_ = os.Remove(path)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func registryDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(registryDirEnv)); dir != "" {
		return dir, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(cache, "pinchtab", "bridges"), nil
}

func entryPath(dir, target string) string {
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}

func readEntry(path string) (Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = f.Close() }()
	var entry Entry
	if err := json.NewDecoder(f).Decode(&entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func live(entry Entry) bool {
	if entry.PID <= 0 {
		return false
	}
	exists, err := process.PidExists(int32(entry.PID))
	if err != nil || !exists {
		return false
	}
	if entry.Port == "" {
		return true
	}
	client := &http.Client{Timeout: 250 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + entry.Port + "/health")
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true
		}
	}
	// The process is authoritative while the bridge is still starting.
	return processMatchesStart(entry)
}

func processMatchesStart(entry Entry) bool {
	p, err := process.NewProcess(int32(entry.PID))
	if err != nil {
		return false
	}
	createdMillis, err := p.CreateTime()
	if err != nil {
		return true
	}
	created := time.UnixMilli(createdMillis)
	return created.Before(entry.StartTime.Add(time.Second)) && created.After(entry.StartTime.Add(-5*time.Second))
}

// ID returns the stable API identifier for an entry.
func (e Entry) ID() string {
	return "bridge_" + strconv.Itoa(e.PID)
}
