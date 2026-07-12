package device

// ScheduleActor adapts Service to schedule.Actor (error-only Shutdown).
type ScheduleActor struct {
	S *Service
}

func (a ScheduleActor) Wake(id string) error { return a.S.Wake(id) }

func (a ScheduleActor) Shutdown(id string) error {
	_, err := a.S.Shutdown(id)
	return err
}

func (a ScheduleActor) WakeGroup(id string) error     { return a.S.WakeGroup(id) }
func (a ScheduleActor) ShutdownGroup(id string) error { return a.S.ShutdownGroup(id) }
