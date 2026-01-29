package storage

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/lhpqaq/ggbot/config"
)

type UserSettings struct {
	OverrideAI *config.AIConfig `json:"override_ai,omitempty"`
}

type Storage struct {
	mu       sync.RWMutex
	path     string
	UserData map[string]*UserSettings `json:"user_data"`
}

func New(path string) (*Storage, error) {
	s := &Storage{
		path:     path,
		UserData: make(map[string]*UserSettings),
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return s, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return s, nil
	}

	err = json.Unmarshal(data, s)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Storage) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}

func (s *Storage) GetUserAIConfig(userID string) *config.AIConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if user, ok := s.UserData[userID]; ok {
		return user.OverrideAI
	}
	return nil
}

func (s *Storage) UpdateUserAIConfig(userID string, cfg config.AIConfig) error {
    s.mu.Lock()
    if _, ok := s.UserData[userID]; !ok {
        s.UserData[userID] = &UserSettings{}
    }
    cfgCopy := cfg
    s.UserData[userID].OverrideAI = &cfgCopy
    s.mu.Unlock()
    
    return s.Save()
}

func (s *Storage) ClearUserAIConfig(userID string) error {
    s.mu.Lock()
    if user, ok := s.UserData[userID]; ok {
        user.OverrideAI = nil
    }
    s.mu.Unlock()
    return s.Save()
}