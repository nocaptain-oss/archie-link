package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateSchemaVersion = 1

// State 是控制面需要落盘的全部可变状态。
// 挑战码是 5 分钟一次性凭据，重启后由客户端重新申请，不持久化。
type State struct {
	SchemaVersion int                      `json:"schema_version"`
	Orders        map[string]*Order        `json:"orders"`
	Subscriptions map[string]*Subscription `json:"subscriptions"`
}

// Store 抽象持久化后端；nil 表示纯内存（测试或本地开发）。
type Store interface {
	Load() (State, error)
	Save(State) error
}

// FileStore 把状态写入单个 JSON 文件：临时文件 -> fsync -> 原子 rename。
// 目录与文件权限分别为 0700 / 0600。
type FileStore struct {
	Path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{Path: path}
}

func (f *FileStore) Load() (State, error) {
	raw, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read controlplane state: %w", err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, fmt.Errorf("decode controlplane state: %w", err)
	}
	if state.SchemaVersion != stateSchemaVersion {
		return State{}, fmt.Errorf("unsupported controlplane state schema version %d", state.SchemaVersion)
	}
	if state.Orders == nil {
		state.Orders = map[string]*Order{}
	}
	if state.Subscriptions == nil {
		state.Subscriptions = map[string]*Subscription{}
	}
	return state, nil
}

func (f *FileStore) Save(state State) error {
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create controlplane state dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create controlplane state temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	encoder := json.NewEncoder(tmp)
	if err := encoder.Encode(state); err != nil {
		tmp.Close()
		return fmt.Errorf("encode controlplane state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync controlplane state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close controlplane state: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod controlplane state: %w", err)
	}
	if err := os.Rename(tmpPath, f.Path); err != nil {
		return fmt.Errorf("replace controlplane state: %w", err)
	}
	dirHandle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open controlplane state dir: %w", err)
	}
	defer dirHandle.Close()
	if err := dirHandle.Sync(); err != nil {
		return fmt.Errorf("sync controlplane state dir: %w", err)
	}
	return nil
}

func emptyState() State {
	return State{
		SchemaVersion: stateSchemaVersion,
		Orders:        map[string]*Order{},
		Subscriptions: map[string]*Subscription{},
	}
}

func (s *Service) cloneStateLocked() State {
	state := emptyState()
	for id, order := range s.orders {
		copied := *order
		state.Orders[id] = &copied
	}
	for id, subscription := range s.subscriptions {
		copied := *subscription
		copied.Devices = make(map[string]Device, len(subscription.Devices))
		for deviceID, device := range subscription.Devices {
			copied.Devices[deviceID] = device
		}
		copied.RegistrationEvents = append([]time.Time(nil), subscription.RegistrationEvents...)
		copied.ProcessedEvents = make(map[string]bool, len(subscription.ProcessedEvents))
		for eventID, processed := range subscription.ProcessedEvents {
			copied.ProcessedEvents[eventID] = processed
		}
		state.Subscriptions[id] = &copied
	}
	return state
}

func (s *Service) restoreStateLocked(state State) {
	s.orders = state.Orders
	s.subscriptions = state.Subscriptions
}

// persistLocked 必须由持有 s.mu 的写方法调用；失败由调用方回滚内存。
func (s *Service) persistLocked() error {
	if s.store == nil {
		return nil
	}
	state := s.cloneStateLocked()
	if err := s.store.Save(state); err != nil {
		return fmt.Errorf("persist controlplane state: %w", err)
	}
	return nil
}
