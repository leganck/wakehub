package clientapp

import (
	"log"
	"sync"
	"time"

	"github.com/leganck/wakehub/internal/protocol"
)

// recentShutdownIDs dedupes remote shutdown requests (process-local).
var recentShutdownIDs sync.Map // requestId -> time.Time

// HandleShutdownMsg is an alias for power-off control messages.
func HandleShutdownMsg(m map[string]any, shutdownCmd string, writeJSON func(any) error) {
	HandleControlMsg(m, "shutdown", shutdownCmd, writeJSON)
}

// HandleControlMsg processes shutdown/restart control messages.
// writeJSON must be concurrency-safe. action is for logging only.
func HandleControlMsg(m map[string]any, action, cmd string, writeJSON func(any) error) {
	reqID, _ := m["requestId"].(string)
	if reqID != "" {
		if _, loaded := recentShutdownIDs.LoadOrStore(reqID, time.Now()); loaded {
			log.Printf("duplicate %s requestId=%s ignored", action, reqID)
			_ = writeJSON(protocol.ShutdownAck{
				Type: protocol.TypeShutdownAck, OK: true,
				Status: protocol.StatusDuplicate, RequestID: reqID,
			})
			return
		}
		go pruneShutdownIDs()
	}

	// ACK first so the server records acceptance before the host reboots/powers off.
	if err := writeJSON(protocol.ShutdownAck{
		Type: protocol.TypeShutdownAck, OK: true,
		Status: protocol.StatusAccepted, RequestID: reqID,
	}); err != nil {
		log.Printf("send %s accepted: %v", action, err)
	}

	log.Printf("exec %s requestId=%s cmd=%s", action, reqID, cmd)
	go func() {
		err := CommandRunner(cmd)
		if err != nil {
			log.Printf("%s error requestId=%s: %v", action, reqID, err)
			if werr := writeJSON(protocol.ShutdownErr{
				Type: protocol.TypeShutdownErr, OK: false,
				Error: err.Error(), RequestID: reqID,
			}); werr != nil {
				log.Printf("send shutdown_err: %v", werr)
			}
			return
		}
		log.Printf("%s command started ok requestId=%s", action, reqID)
		if werr := writeJSON(protocol.ShutdownAck{
			Type: protocol.TypeShutdownAck, OK: true,
			Status: protocol.StatusExecuted, RequestID: reqID,
		}); werr != nil {
			log.Printf("send executed: %v", werr)
		}
	}()
}

// ResetShutdownDedupe clears dedupe state (tests).
func ResetShutdownDedupe() {
	recentShutdownIDs.Range(func(k, _ any) bool {
		recentShutdownIDs.Delete(k)
		return true
	})
}

func pruneShutdownIDs() {
	cutoff := time.Now().Add(-10 * time.Minute)
	recentShutdownIDs.Range(func(k, v any) bool {
		if t, ok := v.(time.Time); ok && t.Before(cutoff) {
			recentShutdownIDs.Delete(k)
		}
		return true
	})
}
