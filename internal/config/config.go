package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	DefaultIntervalMinutes = 15
	MinIntervalMinutes     = 1
	MaxIntervalMinutes     = 60
	DefaultAnkiEndpoint    = "http://127.0.0.1:8765"
)

// ProfileInfo describes a discovered local Anki user profile directory.
type ProfileInfo struct {
	Name           string `json:"name"`
	CollectionPath string `json:"collectionPath"`
	MediaDirPath   string `json:"mediaDirPath"`
}

// DeckFieldSelection stores which note fields the user chose to display on Front and Back for a deck.
type DeckFieldSelection struct {
	FrontFields []string `json:"frontFields"`
	BackFields  []string `json:"backFields"`
}

// Config holds user preferences and daily tracking metadata persisted to ~/.anki-passive-widget/config.json.
type Config struct {
	SelectedProfile   string                        `json:"selectedProfile"`
	SelectedDeck      string                        `json:"selectedDeck"`
	DeckFields        map[string]DeckFieldSelection `json:"deckFields,omitempty"`
	Theme             string                        `json:"theme"`
	IntervalMinutes   int                           `json:"intervalMinutes"`
	AutoPlayAudio     bool                          `json:"autoPlayAudio"`
	OnlyDueDecks      bool                          `json:"onlyDueDecks"`
	MediaDirPath      string                        `json:"mediaDirPath"`
	AnkiEndpoint      string                        `json:"ankiEndpoint"`
	DailyDate         string                        `json:"dailyDate"`
	DailyInitialTotal int                           `json:"dailyInitialTotal"`
}

var fileMu sync.RWMutex

// DefaultConfig returns a Config initialized with sensible defaults.
func DefaultConfig() *Config {
	cfg := &Config{
		SelectedDeck:    "",
		Theme:           "system",
		IntervalMinutes: DefaultIntervalMinutes,
		AutoPlayAudio:   true,
		OnlyDueDecks:    true,
		MediaDirPath:    "",
		AnkiEndpoint:    DefaultAnkiEndpoint,
	}
	cfg.Normalize()
	return cfg
}

// Normalize clamps and populates invalid or empty configuration values.
func (c *Config) Normalize() {
	switch strings.ToLower(strings.TrimSpace(c.Theme)) {
	case "light", "dark", "system":
		c.Theme = strings.ToLower(strings.TrimSpace(c.Theme))
	default:
		c.Theme = "system"
	}
	if c.IntervalMinutes < MinIntervalMinutes {
		if c.IntervalMinutes <= 0 {
			c.IntervalMinutes = DefaultIntervalMinutes
		} else {
			c.IntervalMinutes = MinIntervalMinutes
		}
	}
	if c.IntervalMinutes > MaxIntervalMinutes {
		c.IntervalMinutes = MaxIntervalMinutes
	}
	if strings.TrimSpace(c.AnkiEndpoint) == "" {
		c.AnkiEndpoint = DefaultAnkiEndpoint
	}
	if c.DailyInitialTotal < 0 {
		c.DailyInitialTotal = 0
	}
	if strings.TrimSpace(c.SelectedProfile) == "" {
		if profiles, err := DiscoverProfiles(); err == nil && len(profiles) > 0 {
			c.SelectedProfile = profiles[0].Name
		}
	}
	if c.DeckFields == nil {
		c.DeckFields = make(map[string]DeckFieldSelection)
	}
	for k, sel := range c.DeckFields {
		if sel.FrontFields == nil {
			sel.FrontFields = []string{}
		}
		if sel.BackFields == nil {
			sel.BackFields = []string{}
		}
		c.DeckFields[k] = sel
	}
}

// AnkiBaseDir returns the default OS directory containing Anki profiles.
func AnkiBaseDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			cfgDir, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			appData = cfgDir
		}
		return filepath.Join(appData, "Anki2"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Anki2"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "Anki2"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "Anki2"), nil
	}
}

// DiscoverProfiles scans the local Anki2 folder for profile subdirectories containing collection.anki2.
func DiscoverProfiles() ([]ProfileInfo, error) {
	baseDir, err := AnkiBaseDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	profiles := make([]ProfileInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip internal Anki directories like addons21, logs, debug_scripts, onigiri_backups.
		colPath := filepath.Join(baseDir, name, "collection.anki2")
		if info, statErr := os.Stat(colPath); statErr == nil && !info.IsDir() {
			profiles = append(profiles, ProfileInfo{
				Name:           name,
				CollectionPath: colPath,
				MediaDirPath:   filepath.Join(baseDir, name, "collection.media"),
			})
		}
	}

	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Name) < strings.ToLower(profiles[j].Name)
	})
	return profiles, nil
}

// ResolveActiveProfile finds the ProfileInfo matching selectedProfile, or the first discovered profile.
func ResolveActiveProfile(selectedProfile string) (*ProfileInfo, error) {
	profiles, err := DiscoverProfiles()
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, errors.New("no Anki profiles with collection.anki2 found")
	}

	target := strings.TrimSpace(selectedProfile)
	if target != "" {
		for _, p := range profiles {
			if strings.EqualFold(p.Name, target) {
				copyP := p
				return &copyP, nil
			}
		}
	}
	copyP := profiles[0]
	return &copyP, nil
}

// ConfigPath returns the absolute path to ~/.anki-passive-widget/config.json.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".anki-passive-widget", "config.json"), nil
}

// Load reads the configuration from disk, returning defaults if the file does not yet exist.
func Load() (*Config, error) {
	fileMu.RLock()
	defer fileMu.RUnlock()

	path, err := ConfigPath()
	if err != nil {
		return DefaultConfig(), err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			return cfg, nil
		}
		return DefaultConfig(), err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return DefaultConfig(), err
	}
	cfg.Normalize()
	return cfg, nil
}

// Save writes the configuration to ~/.anki-passive-widget/config.json atomically.
func Save(cfg *Config) error {
	if cfg == nil {
		return errors.New("cannot save nil config")
	}
	cfg.Normalize()

	fileMu.Lock()
	defer fileMu.Unlock()

	path, err := ConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
