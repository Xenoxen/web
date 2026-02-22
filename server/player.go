package server

import (
	"compress/gzip"
	json "github.com/goccy/go-json"
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

// ---- Capture JSON structures ----

// captureEntity maps to an entry in the "entities" array of a capture file.
type captureEntity struct {
	Type        string            `json:"type"`
	ID          int               `json:"id"`
	Name        string            `json:"name"`
	Side        string            `json:"side"`
	IsPlayer    int               `json:"isPlayer"`
	Group       string            `json:"group"`
	Role        string            `json:"role"`
	// Each position entry: [pos, dir, alive, isInVehicle, name, isPlayer]
	Positions [][]json.RawMessage `json:"positions"`
}

// captureData is the top-level structure of a capture JSON file.
type captureData struct {
	WorldName    string          `json:"worldName"`
	MissionName  string          `json:"missionName"`
	EndFrame     int             `json:"endFrame"`
	CaptureDelay float64         `json:"captureDelay"`
	Entities     []captureEntity `json:"entities"`
	// Each event entry: [frameNum, type, victimId, [causedById, weapon], distance]
	Events [][]json.RawMessage `json:"events"`
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

// processPlayerEvents reads a gzip-compressed capture file, parses it, and
// returns a summary of kill/death/weapon statistics for every player unit.
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

	var data captureData
	if err := json.NewDecoder(gz).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode capture JSON: %w", err)
	}

	// Build player map keyed by entity ID.
	type playerAcc struct {
		PlayerEventSummary
		weaponMap map[string]int
	}
	playerMap := make(map[int]*playerAcc, len(data.Entities))
	for _, e := range data.Entities {
		if e.Type != "unit" || e.IsPlayer != 1 {
			continue
		}

		// Resolve display name: walk last 10 positions in reverse for the last non-empty name.
		displayName := e.Name
		start := len(e.Positions) - 10
		if start < 0 {
			start = 0
		}
		for i := len(e.Positions) - 1; i >= start; i-- {
			entry := e.Positions[i]
			if len(entry) >= 5 {
				var name string
				if err := json.Unmarshal(entry[4], &name); err == nil && name != "" {
					displayName = name
					break
				}
			}
		}

		playerMap[e.ID] = &playerAcc{
			PlayerEventSummary: PlayerEventSummary{
				ID:   e.ID,
				Name: displayName,
				Side: e.Side,
			},
			weaponMap: make(map[string]int),
		}
	}

	// Process events — only "killed" events affect stats.
	for _, rawEvent := range data.Events {
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

		// causedByInfo is [causedById, weapon]
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

			// Team kill: killer and victim are different players on the same side.
			if victimIsPlayer && killerID != victimID && killer.Side == victim.Side {
				killer.TeamKillCount++
			}
		}

		if victimIsPlayer {
			victim.DeathCount++
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

	// Sort players by kill count descending for a consistent response order.
	sort.Slice(players, func(i, j int) bool {
		return players[i].KillCount > players[j].KillCount
	})

	return players, nil
}

// fileResult holds the output of processing a single capture file.
type fileResult struct {
	players []PlayerEventSummary
}

// processAllPlayerEvents iterates every .gz file in dataDir concurrently,
// processes player events for each, and merges results by player name.
func processAllPlayerEvents(dataDir string) ([]PlayerEventSummary, error) {
	files, err := filepath.Glob(filepath.Join(dataDir, "*.gz"))
	if err != nil {
		return nil, fmt.Errorf("glob data dir: %w", err)
	}

	totalFiles := len(files)
	log.Printf("[player-cache] processing %d capture files using %d workers", totalFiles, runtime.NumCPU())

	// Process files concurrently with a worker pool.
	results := make([]fileResult, totalFiles)
	var wg sync.WaitGroup
	var processed atomic.Int64
	sem := make(chan struct{}, runtime.NumCPU())

	for i, path := range files {
		wg.Add(1)
		go func(idx int, filePath string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			players, err := processPlayerEvents(filePath)
			if err != nil {
				log.Printf("[player-cache] error processing %s: %v", filepath.Base(filePath), err)
				processed.Add(1)
				return
			}
			results[idx] = fileResult{players: players}

			n := processed.Add(1)
			if n%100 == 0 || n == int64(totalFiles) {
				log.Printf("[player-cache] processed %d/%d files", n, totalFiles)
			}
		}(i, path)
	}
	wg.Wait()

	// Merge all results sequentially (no lock needed, goroutines are done).
	type mergedPlayer struct {
		PlayerEventSummary
		weaponMap map[string]int
	}
	merged := make(map[string]*mergedPlayer)
	for _, r := range results {
		for i := range r.players {
			p := &r.players[i]
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
				continue
			}

			acc.KillCount += p.KillCount
			acc.DeathCount += p.DeathCount
			acc.TeamKillCount += p.TeamKillCount

			for _, ws := range p.WeaponStats {
				acc.weaponMap[ws.Weapon] += ws.Kills
			}
		}
	}

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
	mu       sync.RWMutex
	allStats []PlayerEventSummary
	byName   map[string]*PlayerEventSummary // lowercased full name -> summary
	built    bool
	dataDir  string
}

// NewPlayerCache creates an empty cache for the given data directory.
func NewPlayerCache(dataDir string) *PlayerCache {
	return &PlayerCache{dataDir: dataDir}
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

	stats, err := processAllPlayerEvents(c.dataDir)
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
