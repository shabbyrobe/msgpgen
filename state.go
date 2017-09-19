package msgpgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/shabbyrobe/structer"
)

type StateTypes map[structer.TypeName]int

func (s *StateTypes) UnmarshalJSON(b []byte) error {
	var m map[string]int
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*s = make(StateTypes, len(m))
	for ts, id := range m {
		tn, err := structer.ParseTypeName(ts)
		if err != nil {
			return err
		}
		(*s)[tn] = id
	}
	return nil
}

func (s StateTypes) MarshalJSON() (b []byte, err error) {
	var m = make(map[string]int, len(s))
	for tn, id := range s {
		m[tn.String()] = id
	}
	return json.Marshal(m)
}

func LoadStateFromFile(file string) (*State, error) {
	state := &State{}
	f, err := os.Open(file)
	defer f.Close()

	if !os.IsNotExist(err) {
		if err != nil {
			return nil, err
		}
		if err := json.NewDecoder(f).Decode(state); err != nil {
			return nil, err
		}
	} else {
		state.New = true
	}
	if err := state.Init(); err != nil {
		return nil, err
	}
	return state, nil
}

type State struct {
	Types  StateTypes
	NextID int  `json:"-"`
	New    bool `json:"-"`
}

func (s *State) SaveToFile(file string) (err error) {
	var b []byte
	if b, err = json.Marshal(s); err != nil {
		return
	}
	var out bytes.Buffer
	json.Indent(&out, b, "", "  ")

	var f *os.File
	f, err = os.OpenFile(file, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() {
		if cerr := f.Close(); err != nil {
			err = cerr
		}
	}()
	_, err = io.Copy(f, &out)
	return
}

func (s *State) Init() error {
	if s.Types == nil {
		s.Types = make(StateTypes)
	}

	seen := make(map[int]bool)

	max := 0
	for _, id := range s.Types {
		if id > max {
			max = id
		}
		if seen[id] {
			return fmt.Errorf("duplicate ID %d", id)
		}
		seen[id] = true
	}
	s.NextID = max + 1
	return nil
}

func (s *State) EnsureType(t structer.TypeName) (int, error) {
	if id, ok := s.Types[t]; ok {
		return id, nil
	}
	s.Types[t] = s.NextID
	s.NextID++
	return s.Types[t], nil
}
