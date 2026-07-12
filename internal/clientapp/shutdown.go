package clientapp

import (
	"log"
	"sync"
	"time"

	"github.com/leganck/wakehub/internal/protocol"
)

// recentShutdownIDs dedupes remote shutdown requests (process-local).
var recentShutdownIDs sync.Map // requestId -> time.Time

// HandleShutdownMsg processes a server shutdown control message.
// writeJSON must be concurrency-safe.
func HandleShutdownMsg(m map[string]any, shutdownCmd string, writeJSON func(any) error) {
	reqID, _ := m["requestId"].(string)
	if reqID != "" {
		if _, loaded := recentShutdownIDs.LoadOrStore(reqID, time.Now()); loaded {
			log.Printf("duplicate shutdown requestId=%s ignored", reqID)
			_ = writeJSON(protocol.ShutdownAck{
				Type: protocol.TypeShutdownAck, OK: true,
				Status: protocol.StatusDuplicate, RequestID: reqID,
			})
			return
		}
		go pruneShutdownIDs()
	}

	// ACK first so the server records acceptance before the host powers off.
	if err := writeJSON(protocol.ShutdownAck{
		Type: protocol.TypeShutdownAck, OK: true,
		Status: protocol.StatusAccepted, RequestID: reqID,
	}); err != nil {
		log.Printf("send shutdown accepted: %v", err)
	}

	log.Printf("exec shutdown requestId=%s cmd=%s", reqID, shutdownCmd)
	go func() {
		err := CommandRunner(shutdownCmd)
		if err != nil {
			log.Printf("shutdown error requestId=%s: %v", reqID, err)
			if werr := writeJSON(protocol.ShutdownErr{
				Type: protocol.TypeShutdownErr, OK: false,
				Error: err.Error(), RequestID: reqID,
			}); werr != nil {
				log.Printf("send shutdown_err: %v", werr)
			}
			return
		}
		log.Printf("shutdown command started ok requestId=%s", reqID)
		if werr := writeJSON(protocol.ShutdownAck{
			Type: protocol.TypeShutdownAck, OK: true,
			Status: protocol.StatusExecuted, RequestID: reqID,
		}); werr != nil {
			log.Printf("send shutdown executed: %v", werr)
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
