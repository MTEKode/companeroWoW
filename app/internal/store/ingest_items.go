package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// applyItem applies one message body: msgType, the body m, and v, the same body as it was read (for
// the digest). An item with an oseq is applied once per body digest (ingest_log), and an outbox
// record only when it is newer than the last record stored for its path (outbox_path). A record
// that fails is not recorded, so the next read tries it again. skipped is true when the item was
// applied before or is stale.
func (in *ingester) applyItem(msgType uint8, m map[string]any, v any, charKey string) (skipped bool, err error) {
	oseq := int64(rawNum(m, "oseq"))
	var digest string
	if oseq > 0 {
		d, done, err := in.seen(msgType, oseq, v)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}
		digest = d
	}
	stale, err := in.stale(msgType, m)
	if err != nil {
		return false, err
	}
	if !stale {
		if err := in.apply(msgType, m, charKey); err != nil {
			return false, err
		}
		if err := in.markPath(msgType, m); err != nil {
			return false, err
		}
		// The state of the character is the body the message carries (the record of an outbox item).
		state := m
		if body, _, ok := outboxBody(msgType, m); ok {
			state = body
		}
		if err := in.saveState(msgType, state, v, charKey, in.at, oseq); err != nil {
			return false, err
		}
	}
	if oseq > 0 {
		if _, err := in.tx.Exec(`INSERT OR IGNORE INTO ingest_log (msg_type, oseq, digest, at) VALUES (?, ?, ?, ?)`,
			msgType, oseq, digest, in.now); err != nil {
			return false, fmt.Errorf("log message %d: %w", msgType, err)
		}
	}
	return stale, nil
}

// seen returns the digest of body and whether (msgType, oseq, digest) was applied before.
func (in *ingester) seen(msgType uint8, oseq int64, body any) (digest string, done bool, err error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", false, fmt.Errorf("digest message %d: %w", msgType, err)
	}
	sum := sha256.Sum256(raw)
	digest = hex.EncodeToString(sum[:12])
	var n int
	if err := in.tx.QueryRow(`SELECT COUNT(*) FROM ingest_log WHERE msg_type = ? AND oseq = ? AND digest = ?`,
		msgType, oseq, digest).Scan(&n); err != nil {
		return "", false, fmt.Errorf("read log of message %d: %w", msgType, err)
	}
	return digest, n > 0, nil
}

// outboxPath is the key of an outbox record's path: its elements joined by "|" ("sheet|20000").
func outboxPath(p []any) string {
	parts := make([]string, len(p))
	for i, e := range p {
		parts[i] = pathKey(e)
	}
	return strings.Join(parts, "|")
}

// isOutboxRecord reports whether m is an outbox item: an object with an oseq and a path p.
func isOutboxRecord(m map[string]any) ([]any, bool) {
	p, isPath := m["p"].([]any)
	_, hasSeq := m["oseq"]
	return p, hasSeq && isPath && len(p) > 0
}

// stale reports whether m is an outbox record older than the last record stored for its path. The
// record with the highest oseq of a path is its state (docs/outbox-format.md, section 6).
func (in *ingester) stale(msgType uint8, m map[string]any) (bool, error) {
	p, ok := isOutboxRecord(m)
	if !ok {
		return false, nil
	}
	var old int64
	err := in.tx.QueryRow(`SELECT oseq FROM outbox_path WHERE msg_type = ? AND path = ?`,
		msgType, outboxPath(p)).Scan(&old)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read path of message %d: %w", msgType, err)
	}
	return old >= int64(rawNum(m, "oseq")), nil
}

// markPath records the oseq of an outbox record as the state of its path.
func (in *ingester) markPath(msgType uint8, m map[string]any) error {
	p, ok := isOutboxRecord(m)
	if !ok {
		return nil
	}
	if _, err := in.tx.Exec(`INSERT OR REPLACE INTO outbox_path (msg_type, path, oseq) VALUES (?, ?, ?)`,
		msgType, outboxPath(p), int64(rawNum(m, "oseq"))); err != nil {
		return fmt.Errorf("write path of message %d: %w", msgType, err)
	}
	return nil
}

// decodeKey returns the long name of a key of a compact record: "~" escapes a key (the rest is the
// key as it is), a short key gives its long name, and any other key is kept (docs/outbox-format.md, 4.3).
func decodeKey(k string) string {
	if strings.HasPrefix(k, "~") {
		return k[1:]
	}
	if long, ok := outboxKeys[k]; ok {
		return long
	}
	return k
}
