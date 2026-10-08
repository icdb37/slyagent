package repl

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type MsgStore struct {
	ID      string     `json:"id"`
	History []*Message `json:"history"`
}

type Storer interface {
	Load(id string) (*MsgStore, error)
	Save(*MsgStore) error
}

type Session struct {
	home string // 默认为当前目录
	ids  map[string]struct{}
}

func (s *Session) Load(id string) (*MsgStore, error) {
	path := filepath.Join(s.home, id, "data.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	msg := &MsgStore{}
	err = json.Unmarshal(data, msg)
	return msg, err
}
func (s *Session) Remove(id string) error {
	path := filepath.Join(s.home, id)
	err := os.RemoveAll(path)
	if err != nil {
		return err
	}
	delete(s.ids, id)
	return nil
}

func (s *Session) Save(m *MsgStore) error {
	dir := filepath.Join(s.home, m.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "data.json"), data, 0644)
}

func (s *Session) GetIDs() []string {
	ids := make([]string, 0, len(s.ids))
	for k, _ := range s.ids {
		ids = append(ids, k)
	}
	return ids
}

func NewSession() *Session {
	home, err := os.Getwd() // os.Pwd 不是标准库 API，获取当前工作目录用 os.Getwd
	if err != nil || home == "" {
		home = "."
	}
	home = filepath.Join(home, ".slyagent")
	os.MkdirAll(home, 0755)
	s := &Session{
		home: home,
		ids:  map[string]struct{}{},
	}
	if entries, err := os.ReadDir(home); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				s.ids[e.Name()] = struct{}{}
			}
		}
	}
	return s
}
