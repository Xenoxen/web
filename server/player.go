package server

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

// captureEntityMeta holds only the fields needed for player statistics.
// Unrecognised JSON fields (group, role, framesFired) are silently skipped
// by the decoder, avoiding large allocations for data we never use.
type captureEntityMeta struct {
	Type     string `json:"type"`
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Side     string `json:"side"`
	IsPlayer int    `json:"isPlayer"`
}

// ---- Output structures ----

// PlayerWeaponStat holds kill count per weapon for a player.
type PlayerWeaponStat struct {
	Weapon string `json:"weapon"`
	Kills  int    `json:"kills"`
}

// PlayerEventSummary is the per-player output returned by the endpoint.
type PlayerEventSummary struct {
	ID            int                `json:"id"`
	Name          string             `json:"name"`
	Side          string             `json:"side"`
	KillCount     int                `json:"kill_count"`
	DeathCount    int                `json:"death_count"`
	TeamKillCount int                `json:"team_kill_count"`
	WeaponStats   []PlayerWeaponStat `json:"weapon_stats"`
}

// ---- Core logic ----

// processPlayerEvents reads a gzip-compressed capture file using a streaming
// JSON decoder. Only one entity or event is in memory at a time, avoiding the
// need to deserialise the entire (often huge) capture into a single struct.
func processPlayerEvents(path string) ([]PlayerEventSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open capture file: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gz.Close()

	dec := json.NewDecoder(gz)

	// Read opening '{'.
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("read opening brace: %w", err)
	}

	type playerAcc struct {
		PlayerEventSummary
		weaponMap map[string]int
	}
	playerMap := make(map[int]*playerAcc)

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("read key: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			continue
		}

		switch key {
		case "entities":
			// Read opening '['.
			if _, err := dec.Token(); err != nil {
				return nil, fmt.Errorf("read entities start: %w", err)
			}
			// Stream one entity at a time — each is decoded, inspected, then discarded.
			for dec.More() {
				var e captureEntityMeta
				if err := dec.Decode(&e); err != nil {
					return nil, fmt.Errorf("decode entity: %w", err)
				}
				if e.Type != "unit" || e.IsPlayer != 1 {
					continue
				}

				playerMap[e.ID] = &playerAcc{
					PlayerEventSummary: PlayerEventSummary{
						ID:   e.ID,
						Name: e.Name,
						Side: e.Side,
					},
					weaponMap: make(map[string]int),
				}
			}
			// Read closing ']'.
			if _, err := dec.Token(); err != nil {
				return nil, fmt.Errorf("read entities end: %w", err)
			}

		case "events":
			// Read opening '['.
			if _, err := dec.Token(); err != nil {
				return nil, fmt.Errorf("read events start: %w", err)
			}
			for dec.More() {
				var rawEvent []json.RawMessage
				if err := dec.Decode(&rawEvent); err != nil {
					return nil, fmt.Errorf("decode event: %w", err)
				}
				if len(rawEvent) < 4 {
					continue
				}

				var eventType string
				if err := json.Unmarshal(rawEvent[1], &eventType); err != nil || eventType != "killed" {
					continue
				}

				var victimID int
				if err := json.Unmarshal(rawEvent[2], &victimID); err != nil {
					continue
				}

				var causedByInfo []json.RawMessage
				if err := json.Unmarshal(rawEvent[3], &causedByInfo); err != nil || len(causedByInfo) < 2 {
					continue
				}

				var killerID int
				if err := json.Unmarshal(causedByInfo[0], &killerID); err != nil {
					continue
				}

				weapon := "N/A"
				if err := json.Unmarshal(causedByInfo[1], &weapon); err != nil {
					weapon = "N/A"
				}

				killer, killerIsPlayer := playerMap[killerID]
				victim, victimIsPlayer := playerMap[victimID]

				if killerIsPlayer {
					killer.KillCount++
					killer.weaponMap[weapon]++

					if victimIsPlayer && killerID != victimID && killer.Side == victim.Side {
						killer.TeamKillCount++
					}
				}

				if victimIsPlayer {
					victim.DeathCount++
				}
			}
			// Read closing ']'.
			if _, err := dec.Token(); err != nil {
				return nil, fmt.Errorf("read events end: %w", err)
			}

		default:
			// Skip unknown top-level fields (worldName, missionName, etc.).
			var discard json.RawMessage
			if err := dec.Decode(&discard); err != nil {
				return nil, fmt.Errorf("skip field %s: %w", key, err)
			}
		}
	}

	// Collect results: convert weapon maps to sorted slices.
	players := make([]PlayerEventSummary, 0, len(playerMap))
	for _, p := range playerMap {
		ws := make([]PlayerWeaponStat, 0, len(p.weaponMap))
		for weapon, kills := range p.weaponMap {
			ws = append(ws, PlayerWeaponStat{Weapon: weapon, Kills: kills})
		}
		sort.Slice(ws, func(i, j int) bool {
			return ws[i].Kills > ws[j].Kills
		})
		p.WeaponStats = ws
		players = append(players, p.PlayerEventSummary)
	}

	sort.Slice(players, func(i, j int) bool {
		return players[i].KillCount > players[j].KillCount
	})

	return players, nil
}

// processAllPlayerEvents iterates every .gz file in dataDir concurrently,
// processes player events for each, and merges results by player name.
// Results are merged incrementally under a mutex so that per-file summaries
// can be garbage-collected as soon as they are folded in.
func processAllPlayerEvents(dataDir string, blacklist []string) ([]PlayerEventSummary, error) {
	allFiles, err := filepath.Glob(filepath.Join(dataDir, "*.gz"))
	if err != nil {
		return nil, fmt.Errorf("glob data dir: %w", err)
	}

	// Filter out blacklisted filenames (case-insensitive substring match against name without .gz).
	files := allFiles[:0]
	for _, f := range allFiles {
		name := strings.ToLower(strings.TrimSuffix(filepath.Base(f), ".gz"))
		excluded := false
		for _, b := range blacklist {
			if strings.Contains(name, strings.ToLower(b)) {
				excluded = true
				break
			}
		}
		if !excluded {
			files = append(files, f)
		}
	}

	totalFiles := len(files)
	log.Printf("[player-cache] processing %d capture files using %d workers", totalFiles, runtime.NumCPU())

	// Shared merge map — each worker merges its results immediately.
	type mergedPlayer struct {
		PlayerEventSummary
		weaponMap map[string]int
	}
	var mu sync.Mutex
	merged := make(map[string]*mergedPlayer)

	var wg sync.WaitGroup
	var processed atomic.Int64
	sem := make(chan struct{}, runtime.NumCPU())

	for _, path := range files {
		wg.Add(1)
		go func(filePath string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			players, err := processPlayerEvents(filePath)
			if err != nil {
				log.Printf("[player-cache] error processing %s: %v", filepath.Base(filePath), err)
				processed.Add(1)
				return
			}

			// Merge into shared map immediately so per-file data can be freed.
			mu.Lock()
			for i := range players {
				p := &players[i]
				acc, exists := merged[p.Name]
				if !exists {
					wm := make(map[string]int, len(p.WeaponStats))
					for _, ws := range p.WeaponStats {
						wm[ws.Weapon] = ws.Kills
					}
					merged[p.Name] = &mergedPlayer{
						PlayerEventSummary: PlayerEventSummary{
							ID:            p.ID,
							Name:          p.Name,
							Side:          p.Side,
							KillCount:     p.KillCount,
							DeathCount:    p.DeathCount,
							TeamKillCount: p.TeamKillCount,
						},
						weaponMap: wm,
					}
				} else {
					acc.KillCount += p.KillCount
					acc.DeathCount += p.DeathCount
					acc.TeamKillCount += p.TeamKillCount
					for _, ws := range p.WeaponStats {
						acc.weaponMap[ws.Weapon] += ws.Kills
					}
				}
			}
			mu.Unlock()

			n := processed.Add(1)
			if n%100 == 0 || n == int64(totalFiles) {
				log.Printf("[player-cache] processed %d/%d files", n, totalFiles)
			}
		}(path)
	}
	wg.Wait()

	result := make([]PlayerEventSummary, 0, len(merged))
	for _, m := range merged {
		ws := make([]PlayerWeaponStat, 0, len(m.weaponMap))
		for weapon, kills := range m.weaponMap {
			ws = append(ws, PlayerWeaponStat{Weapon: weapon, Kills: kills})
		}
		sort.Slice(ws, func(i, j int) bool {
			return ws[i].Kills > ws[j].Kills
		})
		m.WeaponStats = ws
		result = append(result, m.PlayerEventSummary)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].KillCount > result[j].KillCount
	})

	return result, nil
}

// ---- Player Cache ----

// PlayerCache holds precomputed player statistics so that repeated HTTP
// requests do not re-parse every capture file.
type PlayerCache struct {
	mu        sync.RWMutex
	allStats  []PlayerEventSummary
	byName    map[string]*PlayerEventSummary // lowercased full name -> summary
	built     bool
	dataDir   string
	blacklist []string
}

// NewPlayerCache creates an empty cache for the given data directory.
// blacklist is a list of capture filenames (without .gz) to exclude.
func NewPlayerCache(dataDir string, blacklist []string) *PlayerCache {
	return &PlayerCache{dataDir: dataDir, blacklist: blacklist}
}

// ensureBuilt lazily builds the cache on first access or after invalidation.
func (c *PlayerCache) ensureBuilt() error {
	// Fast path: already built.
	c.mu.RLock()
	if c.built {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	// Slow path: acquire write lock and rebuild.
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock.
	if c.built {
		return nil
	}

	log.Println("[player-cache] building cache...")
	start := time.Now()

	stats, err := processAllPlayerEvents(c.dataDir, c.blacklist)
	if err != nil {
		return err
	}

	byName := make(map[string]*PlayerEventSummary, len(stats))
	for i := range stats {
		byName[strings.ToLower(stats[i].Name)] = &stats[i]
	}

	c.allStats = stats
	c.byName = byName
	c.built = true

	log.Printf("[player-cache] cache built in %s — %d unique players", time.Since(start).Round(time.Millisecond), len(stats))
	return nil
}

// GetAll returns aggregated stats for every player across all captures.
func (c *PlayerCache) GetAll() ([]PlayerEventSummary, error) {
	if err := c.ensureBuilt(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.allStats, nil
}

// GetByName returns aggregated stats for a single player (case-insensitive
// substring match). Returns nil if no player matches.
func (c *PlayerCache) GetByName(playerName string) (*PlayerEventSummary, error) {
	if err := c.ensureBuilt(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalised := strings.ToLower(playerName)

	// Try exact match first.
	if p, ok := c.byName[normalised]; ok {
		return p, nil
	}

	// Fall back to substring match.
	for key, p := range c.byName {
		if strings.Contains(key, normalised) {
			return p, nil
		}
	}

	return nil, nil
}

// Invalidate marks the cache as stale so the next request triggers a rebuild.
func (c *PlayerCache) Invalidate() {
	c.mu.Lock()
	c.built = false
	c.allStats = nil
	c.byName = nil
	c.mu.Unlock()
	log.Println("[player-cache] cache invalidated")
}

// ---- HTTP handler ----

// GetAllPlayerStats handles GET /api/v1/players
// It aggregates kill/death/weapon statistics for every player across all captures.
func (h *Handler) GetAllPlayerStats(c echo.Context) error {
	players, err := h.playerCache.GetAll()
	if err != nil {
		return fmt.Errorf("process all player events: %w", err)
	}

	return c.JSONPretty(http.StatusOK, players, "\t")
}

// GetPlayerStatsByName handles GET /api/v1/players/:name
// It returns aggregated statistics for a single named player across all captures.
func (h *Handler) GetPlayerStatsByName(c echo.Context) error {
	playerName, err := url.PathUnescape(c.Param("name"))
	if err != nil {
		return err
	}

	player, err := h.playerCache.GetByName(playerName)
	if err != nil {
		return fmt.Errorf("process player events by name: %w", err)
	}
	if player == nil {
		return echo.ErrNotFound
	}

	return c.JSONPretty(http.StatusOK, player, "\t")
}

// GetPlayerEvents handles GET /api/v1/captures/:name/players
// It returns a JSON array of PlayerEventSummary for every player in the capture.
func (h *Handler) GetPlayerEvents(c echo.Context) error {
	name, err := url.PathUnescape(c.Param("name"))
	if err != nil {
		return err
	}

	path := filepath.Join(h.setting.Data, filepath.Base(name+".gz"))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return echo.ErrNotFound
	}

	players, err := processPlayerEvents(path)
	if err != nil {
		return fmt.Errorf("process player events: %w", err)
	}

	return c.JSONPretty(http.StatusOK, players, "\t")
}
