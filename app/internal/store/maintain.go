package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"
)

// Retention. Chat history keeps the newest keepChatMessages messages. Cached web pages
// and search results expire after their own maximum age, and VACUUM runs at most once
// per vacuumEvery, so the database does not grow without bound.
const (
	keepChatMessages  = 500
	pageCacheMaxAge   = 7 * 24 * time.Hour
	searchCacheMaxAge = 24 * time.Hour
	vacuumEvery       = 7 * 24 * time.Hour
	// lastVacuumKey stores, in the cache table, the time of the last VACUUM as Unix
	// milliseconds. Its key matches none of the expiring prefixes, so it is never trimmed.
	lastVacuumKey = "store:last_vacuum"
)

// MaintenanceResult says what one Maintain run removed.
type MaintenanceResult struct {
	MessagesDeleted  int64
	CacheDeleted     int64
	CombatsDeleted   int64
	SnapshotsDeleted int64
	Vacuumed         bool
}

// Maintain trims the stored data: it keeps the newest keepChatMessages chat messages,
// deletes expired page and search cache rows, and runs VACUUM when the last one is at
// least vacuumEvery old.
func (s *Store) Maintain() (MaintenanceResult, error) {
	var res MaintenanceResult
	now := s.now()

	n, err := s.trimMessages(keepChatMessages)
	if err != nil {
		return res, err
	}
	res.MessagesDeleted = n

	n, err = s.expireCache(now)
	if err != nil {
		return res, err
	}
	res.CacheDeleted = n

	res.CombatsDeleted, res.SnapshotsDeleted, err = s.trimHistory(now)
	if err != nil {
		return res, err
	}

	due, err := s.vacuumDue(now)
	if err != nil {
		return res, err
	}
	if due {
		if _, err := s.db.Exec(`VACUUM`); err != nil {
			return res, fmt.Errorf("vacuum: %w", err)
		}
		if _, err := s.db.Exec(
			`INSERT OR REPLACE INTO cache (key, value, fetched_at) VALUES (?, ?, ?)`,
			lastVacuumKey, strconv.FormatInt(now.UnixMilli(), 10), now.UnixMilli()); err != nil {
			return res, fmt.Errorf("record vacuum time: %w", err)
		}
		res.Vacuumed = true
	}
	return res, nil
}

// trimMessages deletes all but the newest keep chat messages.
func (s *Store) trimMessages(keep int) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY id DESC LIMIT ?)`, keep)
	if err != nil {
		return 0, fmt.Errorf("trim messages: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// expireCache deletes page rows older than pageCacheMaxAge and search rows older than
// searchCacheMaxAge.
func (s *Store) expireCache(now time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM cache WHERE (key LIKE 'page:%' AND fetched_at < ?) OR (key LIKE 'search:%' AND fetched_at < ?)`,
		now.Add(-pageCacheMaxAge).UnixMilli(), now.Add(-searchCacheMaxAge).UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("expire cache: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// vacuumDue reports whether no VACUUM has run in the last vacuumEvery.
func (s *Store) vacuumDue(now time.Time) (bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM cache WHERE key = ?`, lastVacuumKey).Scan(&value)
	if err != nil {
		// No row yet (or an unreadable one): a VACUUM is due.
		return true, nil
	}
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return true, nil
	}
	return now.Sub(time.UnixMilli(ms)) >= vacuumEvery, nil
}

// RunMaintenance runs Maintain now and then every interval until ctx ends. Each run that
// removes something, or vacuums, is logged. Errors are logged and do not stop the loop.
func (s *Store) RunMaintenance(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Hour
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.maintainOnce()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Store) maintainOnce() {
	res, err := s.Maintain()
	if err != nil {
		log.Printf("store: mantenimiento: %v", err)
		return
	}
	if res.MessagesDeleted > 0 || res.CacheDeleted > 0 || res.Vacuumed {
		log.Printf("store: mantenimiento: %d mensajes, %d entradas de caché eliminados; VACUUM: %t",
			res.MessagesDeleted, res.CacheDeleted, res.Vacuumed)
	}
}
