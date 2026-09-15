package dumper

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"database-dumper-server/db"
)

const (
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusFailed      = "failed"
	StatusStopped     = "stopped"
	StatusInterrupted = "interrupted"
)

type Job struct {
	ID        int64
	ServerID  string
	StartedAt time.Time

	mu     sync.Mutex
	status string
	endAt  time.Time
	err    string
	files  []string
	log    strings.Builder
	dirty  bool
	cancel context.CancelFunc
}

func (j *Job) Printf(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.log.WriteString(fmt.Sprintf(format, args...))
	j.log.WriteString("\n")
	j.dirty = true
}

func (j *Job) Writer() io.Writer {
	return jobWriter{j}
}

type jobWriter struct{ j *Job }

func (w jobWriter) Write(p []byte) (int, error) {
	w.j.mu.Lock()
	defer w.j.mu.Unlock()
	w.j.log.Write(p)
	w.j.dirty = true
	return len(p), nil
}

type Snapshot struct {
	ID        int64
	ServerID  string
	Status    string
	StartedAt time.Time
	EndedAt   time.Time
	Error     string
	Files     []string
	Log       string
	Finished  bool
}

func (j *Job) record() db.JobRecord {
	return db.JobRecord{
		ID:        j.ID,
		ServerID:  j.ServerID,
		Status:    j.status,
		StartedAt: j.StartedAt,
		EndedAt:   j.endAt,
		Error:     j.err,
		Files:     append([]string{}, j.files...),
		Log:       j.log.String(),
	}
}

func (j *Job) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return snapshotFromRecord(j.record())
}

func snapshotFromRecord(r db.JobRecord) Snapshot {
	return Snapshot{
		ID:        r.ID,
		ServerID:  r.ServerID,
		Status:    r.Status,
		StartedAt: r.StartedAt,
		EndedAt:   r.EndedAt,
		Error:     r.Error,
		Files:     r.Files,
		Log:       r.Log,
		Finished:  r.Status != StatusRunning,
	}
}

func (j *Job) persist(force bool) {
	j.mu.Lock()
	if !force && !j.dirty {
		j.mu.Unlock()
		return
	}
	rec := j.record()
	j.dirty = false
	j.mu.Unlock()
	if err := db.UpdateJob(rec); err != nil {
		log.Printf("job %d: persist: %v", j.ID, err)
	}
}

func (j *Job) Stop() {
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

type Manager struct {
	mu      sync.Mutex
	running map[int64]*Job
}

func NewManager() *Manager {
	return &Manager{running: map[int64]*Job{}}
}

func (m *Manager) Start(server db.Server, opts Options) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, j := range m.running {
		if j.ServerID == server.ID {
			return nil, fmt.Errorf("a dump for %s is already running", server.ID)
		}
	}

	started := time.Now()
	id, err := db.InsertJob(server.ID, StatusRunning, started)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: id, ServerID: server.ID, StartedAt: started, status: StatusRunning, cancel: cancel}
	m.running[id] = job

	go m.run(ctx, cancel, job, server, opts)
	return job, nil
}

func (m *Manager) run(ctx context.Context, cancel context.CancelFunc, job *Job, server db.Server, opts Options) {
	defer cancel()

	flush := time.NewTicker(time.Second)
	defer flush.Stop()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-flush.C:
				job.persist(false)
			case <-done:
				return
			}
		}
	}()

	res, err := Run(ctx, server, opts, job)
	close(done)

	job.mu.Lock()
	job.endAt = time.Now()
	job.files = res.Files
	switch {
	case ctx.Err() != nil:
		job.status = StatusStopped
		job.err = "stopped by user"
		job.log.WriteString("Stopped by user\n")
	case err != nil:
		job.status = StatusFailed
		job.err = err.Error()
		job.log.WriteString("Error: " + err.Error() + "\n")
	default:
		job.status = StatusDone
		job.log.WriteString("Dump finished successfully\n")
	}
	job.mu.Unlock()

	job.persist(true)

	m.mu.Lock()
	delete(m.running, job.ID)
	m.mu.Unlock()
}

func (m *Manager) Running(id int64) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.running[id]
	return j, ok
}

func (m *Manager) Get(id int64) (Snapshot, bool) {
	if j, ok := m.Running(id); ok {
		return j.Snapshot(), true
	}
	rec, err := db.GetJob(id)
	if err != nil {
		return Snapshot{}, false
	}
	return snapshotFromRecord(rec), true
}

func (m *Manager) List(u db.User, limit int) ([]Snapshot, error) {
	recs, err := db.ListJobsFor(u, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(recs))
	for _, r := range recs {
		if j, ok := m.Running(r.ID); ok {
			out = append(out, j.Snapshot())
			continue
		}
		out = append(out, snapshotFromRecord(r))
	}
	return out, nil
}
