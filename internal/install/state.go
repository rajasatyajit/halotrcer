package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// State records what Halotrcer itself installed, so `halotrcer uninstall
// --all` removes only those and never a tool the user installed separately.
type State struct {
	Version int               `json:"version"`
	Tools   map[string]Record `json:"tools"`
}

// Record is one tool Halotrcer installed.
type Record struct {
	Channel     string    `json:"channel"`
	Path        string    `json:"path,omitempty"`
	Version     string    `json:"version,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// StatePath is ~/.local/state/halotrcer/installed.json ($XDG_STATE_HOME
// when set), or %LOCALAPPDATA%\halotrcer\installed.json on Windows.
func (s System) StatePath() string {
	if s.OS == "windows" {
		base := s.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(s.Home, "AppData", "Local")
		}
		return filepath.Join(base, "halotrcer", "installed.json")
	}
	if x := s.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "halotrcer", "installed.json")
	}
	return filepath.Join(s.Home, ".local", "state", "halotrcer", "installed.json")
}

// LoadState reads the state file; a missing file is an empty state.
func LoadState(path string) (*State, error) {
	st := &State{Version: 1, Tools: map[string]Record{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if st.Tools == nil {
		st.Tools = map[string]Record{}
	}
	return st, nil
}

// Save writes the state file atomically, mode 0600.
func (st *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".installed-*.json")
	if err != nil {
		return err
	}
	tn := tmp.Name()
	defer func() { _ = os.Remove(tn) }()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tn, path)
}
