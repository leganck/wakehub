package schedule

import (
	"context"
	"log/slog"
	"time"

	"github.com/leganck/wakehub/internal/config"
)

// Actor performs wake/shutdown for schedules.
// Shutdown may ignore request IDs; only the error is used.
type Actor interface {
	Wake(id string) error
	// Shutdown is error-only for schedule compatibility (requestId discarded by adapters).
	Shutdown(id string) error
	WakeGroup(id string) error
	ShutdownGroup(id string) error
}

// ErrorShutdown adapts a function that returns (requestId, error) to Actor.Shutdown.
type ErrorShutdown func(id string) error

type Notifier interface {
	OnSchedule(action, targetType, targetID, targetName string, ok bool, errMsg string)
}

type Runner struct {
	store *config.Store
	actor Actor
	note  Notifier
}

func New(store *config.Store, actor Actor, note Notifier) *Runner {
	return &Runner{store: store, actor: actor, note: note}
}

func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	r.tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick()
		}
	}
}

func (r *Runner) tick() {
	now := time.Now()
	day := now.Format("2006-01-02")
	wd := int(now.Weekday()) // 0=Sunday
	h, m := now.Hour(), now.Minute()

	for _, sc := range r.store.ListSchedules() {
		if !sc.Enabled {
			continue
		}
		if sc.LastRunDay == day {
			continue
		}
		if sc.Hour != h || sc.Minute != m {
			continue
		}
		if len(sc.Weekdays) > 0 && !containsInt(sc.Weekdays, wd) {
			continue
		}
		r.fire(sc, day)
	}
}

func (r *Runner) fire(sc config.Schedule, day string) {
	var err error
	targetType, targetID, targetName := "device", sc.DeviceID, sc.Name
	if sc.GroupID != "" {
		targetType, targetID = "group", sc.GroupID
		if g, ok := r.store.GetGroup(sc.GroupID); ok {
			targetName = g.Name
		}
		if sc.Action == "wake" {
			err = r.actor.WakeGroup(sc.GroupID)
		} else {
			err = r.actor.ShutdownGroup(sc.GroupID)
		}
	} else {
		if d, ok := r.store.GetDevice(sc.DeviceID); ok {
			targetName = d.Name
		}
		if sc.Action == "wake" {
			err = r.actor.Wake(sc.DeviceID)
		} else {
			err = r.actor.Shutdown(sc.DeviceID)
		}
	}
	_ = r.store.MarkScheduleRun(sc.ID, day)
	msg := ""
	ok := err == nil
	if err != nil {
		msg = err.Error()
		slog.Error("schedule failed", "id", sc.ID, "action", sc.Action, "err", err)
	} else {
		slog.Info("schedule ok", "id", sc.ID, "action", sc.Action)
	}
	if r.note != nil {
		r.note.OnSchedule(sc.Action, targetType, targetID, targetName, ok, msg)
	}
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
