package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// WFTPage is a page of wowforevertalents.com as last fetched: its raw HTML and the
// validators and patch needed to refresh it cheaply.
type WFTPage struct {
	URL          string
	Body         string
	ETag         string
	LastModified string
	Patch        string
	FetchedAt    time.Time
}

// WFTURL is one entry of the site's sitemap, with the title and section derived from its path.
type WFTURL struct {
	URL     string
	Slug    string
	Title   string
	Section string // "abilities", "talents", "builds", "dungeons", "professions", ...
	Class   string // class slug when the path names one ("priest"), else ""
}

// WFTPageGet returns the stored page for url, or ok=false when there is none.
func (s *Store) WFTPageGet(url string) (WFTPage, bool, error) {
	var p WFTPage
	var fetched int64
	err := s.db.QueryRow(`SELECT url, body, etag, last_modified, patch, fetched_at FROM wft_pages WHERE url = ?`, url).
		Scan(&p.URL, &p.Body, &p.ETag, &p.LastModified, &p.Patch, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return WFTPage{}, false, nil
	}
	if err != nil {
		return WFTPage{}, false, fmt.Errorf("read wft page %q: %w", url, err)
	}
	p.FetchedAt = time.UnixMilli(fetched)
	return p, true, nil
}

// WFTPagePut stores or replaces a page. A zero FetchedAt means now.
func (s *Store) WFTPagePut(p WFTPage) error {
	at := p.FetchedAt
	if at.IsZero() {
		at = s.now()
	}
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO wft_pages (url, body, etag, last_modified, patch, fetched_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.URL, p.Body, p.ETag, p.LastModified, p.Patch, at.UnixMilli()); err != nil {
		return fmt.Errorf("write wft page %q: %w", p.URL, err)
	}
	return nil
}

// WFTPageTouch refreshes the fetch time and patch of a stored page (after a 304 Not Modified).
func (s *Store) WFTPageTouch(url, patch string) error {
	if _, err := s.db.Exec(`UPDATE wft_pages SET fetched_at = ?, patch = ? WHERE url = ?`,
		s.now().UnixMilli(), patch, url); err != nil {
		return fmt.Errorf("touch wft page %q: %w", url, err)
	}
	return nil
}

// WFTMetaGet returns a metadata value, or "" when it is not set.
func (s *Store) WFTMetaGet(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM wft_meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read wft meta %q: %w", key, err)
	}
	return v, nil
}

// WFTMetaSet stores a metadata value.
func (s *Store) WFTMetaSet(key, value string) error {
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO wft_meta (key, value) VALUES (?, ?)`, key, value); err != nil {
		return fmt.Errorf("write wft meta %q: %w", key, err)
	}
	return nil
}

// WFTSitemapReplace replaces the whole sitemap index with urls, in one transaction.
func (s *Store) WFTSitemapReplace(urls []WFTURL) error {
	return s.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM wft_sitemap`); err != nil {
			return fmt.Errorf("clear wft sitemap: %w", err)
		}
		for _, u := range urls {
			if _, err := tx.Exec(`INSERT OR REPLACE INTO wft_sitemap (url, slug, title, section, cls) VALUES (?, ?, ?, ?, ?)`,
				u.URL, u.Slug, u.Title, u.Section, u.Class); err != nil {
				return fmt.Errorf("write wft sitemap entry %q: %w", u.URL, err)
			}
		}
		return nil
	})
}

// WFTSitemapAll returns every indexed URL.
func (s *Store) WFTSitemapAll() ([]WFTURL, error) {
	rows, err := s.db.Query(`SELECT url, slug, title, section, cls FROM wft_sitemap ORDER BY url`)
	if err != nil {
		return nil, fmt.Errorf("read wft sitemap: %w", err)
	}
	defer rows.Close()
	var out []WFTURL
	for rows.Next() {
		var u WFTURL
		if err := rows.Scan(&u.URL, &u.Slug, &u.Title, &u.Section, &u.Class); err != nil {
			return nil, fmt.Errorf("scan wft sitemap: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// WFTParsedGet returns the cached structured data stored under key (JSON text).
func (s *Store) WFTParsedGet(key string) (string, string, time.Time, bool, error) {
	var js, patch string
	var fetched int64
	err := s.db.QueryRow(`SELECT json, patch, fetched_at FROM wft_parsed WHERE key = ?`, key).Scan(&js, &patch, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", time.Time{}, false, nil
	}
	if err != nil {
		return "", "", time.Time{}, false, fmt.Errorf("read wft parsed %q: %w", key, err)
	}
	return js, patch, time.UnixMilli(fetched), true, nil
}

// WFTParsedPut stores structured data (JSON text) under key, with the patch it was parsed under.
func (s *Store) WFTParsedPut(key, kind, js, patch string) error {
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO wft_parsed (key, kind, json, patch, fetched_at) VALUES (?, ?, ?, ?, ?)`,
		key, kind, js, patch, s.now().UnixMilli()); err != nil {
		return fmt.Errorf("write wft parsed %q: %w", key, err)
	}
	return nil
}
