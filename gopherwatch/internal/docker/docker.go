package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type engineAPI interface {
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error
}

type Manager struct {
	api    engineAPI
	logger *slog.Logger
	closer io.Closer

	now         func() time.Time
	stopTimeout time.Duration
	opTimeout   time.Duration

	mu          sync.Mutex
	locks       map[string]*sync.Mutex
	lastRestart map[string]time.Time
}

type Option func(*Manager)

func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

func WithStopTimeout(d time.Duration) Option {
	return func(m *Manager) {
		m.stopTimeout = d
	}
}

func WithOpTimeout(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.opTimeout = d
		}
	}
}

func New(logger *slog.Logger, opts ...Option) (*Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client oluşturulamadı: %w", err)
	}
	m := newManager(cli, logger, opts...)
	m.closer = cli
	return m, nil
}

func newManager(api engineAPI, logger *slog.Logger, opts ...Option) *Manager {
	m := &Manager{
		api:         api,
		logger:      logger,
		now:         time.Now,
		stopTimeout: 10 * time.Second,
		opTimeout:   30 * time.Second,
		locks:       make(map[string]*sync.Mutex),
		lastRestart: make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Manager) Close() error {
	if m.closer != nil {
		return m.closer.Close()
	}
	return nil
}

func (m *Manager) Restart(ctx context.Context, containerName string, cooldown time.Duration) (bool, error) {

	lock := m.lockFor(containerName)
	lock.Lock()
	defer lock.Unlock()

	if cooldown > 0 {
		if last := m.getLastRestart(containerName); !last.IsZero() {
			if elapsed := m.now().Sub(last); elapsed < cooldown {
				m.logger.Info("restart atlandı: cooldown aktif",
					"container", containerName,
					"elapsed", elapsed.Round(time.Second),
					"cooldown", cooldown)
				return false, nil
			}
		}
	}

	m.setLastRestart(containerName, m.now())

	opCtx, cancel := context.WithTimeout(ctx, m.opTimeout)
	defer cancel()

	info, err := m.api.ContainerInspect(opCtx, containerName)
	if err != nil {
		return false, fmt.Errorf("konteyner inspect başarısız (%s): %w", containerName, err)
	}

	id, running := containerName, false
	if base := info.ContainerJSONBase; base != nil {
		id = short(base.ID)
		if base.State != nil {
			running = base.State.Running
		}
	}
	m.logger.Info("konteyner restart ediliyor",
		"container", containerName, "id", id, "running", running)

	timeoutSecs := int(m.stopTimeout.Seconds())
	if err := m.api.ContainerRestart(opCtx, containerName, container.StopOptions{Timeout: &timeoutSecs}); err != nil {
		return false, fmt.Errorf("konteyner restart başarısız (%s): %w", containerName, err)
	}
	return true, nil
}

func (m *Manager) lockFor(name string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock, ok := m.locks[name]
	if !ok {
		lock = &sync.Mutex{}
		m.locks[name] = lock
	}
	return lock
}

func (m *Manager) getLastRestart(name string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRestart[name]
}

func (m *Manager) setLastRestart(name string, t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRestart[name] = t
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
