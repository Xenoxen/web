package server

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	FramesFired []json.RawMessage `json:"framesFired"`
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
	playerMap := make(map[int]*PlayerEventSummary, len(data.Entities))
	for _, e := range data.Entities {
		if e.Type != "unit" || e.IsPlayer != 1 {
			continue
		}

		// Resolve display name: walk positions in reverse for the last non-empty name.
		displayName := e.Name
		for i := len(e.Positions) - 1; i >= 0; i-- {
			entry := e.Positions[i]
			if len(entry) >= 5 {
				var name string
				if err := json.Unmarshal(entry[4], &name); err == nil && name != "" {
					displayName = name
					break
				}
			}
		}

		playerMap[e.ID] = &PlayerEventSummary{
			ID:          e.ID,
			Name:        displayName,
			Side:        e.Side,
			WeaponStats: []PlayerWeaponStat{},
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

			// Accumulate weapon kill stats.
			found := false
			for i := range killer.WeaponStats {
				if killer.WeaponStats[i].Weapon == weapon {
					killer.WeaponStats[i].Kills++
					found = true
					break
				}
			}
			if !found {
				killer.WeaponStats = append(killer.WeaponStats, PlayerWeaponStat{Weapon: weapon, Kills: 1})
			}

			// Team kill: killer and victim are different players on the same side.
			if victimIsPlayer && killerID != victimID && killer.Side == victim.Side {
				killer.TeamKillCount++
			}
		}

		if victimIsPlayer {
			victim.DeathCount++
		}
	}

	// Collect results and sort weapon stats by kills descending.
	players := make([]PlayerEventSummary, 0, len(playerMap))
	for _, p := range playerMap {
		sort.Slice(p.WeaponStats, func(i, j int) bool {
			return p.WeaponStats[i].Kills > p.WeaponStats[j].Kills
		})
		players = append(players, *p)
	}

	// Sort players by kill count descending for a consistent response order.
	sort.Slice(players, func(i, j int) bool {
		return players[i].KillCount > players[j].KillCount
	})

	return players, nil
}

// processAllPlayerEvents iterates every .gz file in dataDir, processes player
// events for each, and merges results by player name across all captures.
func processAllPlayerEvents(dataDir string) ([]PlayerEventSummary, error) {
	files, err := filepath.Glob(filepath.Join(dataDir, "*.gz"))
	if err != nil {
		return nil, fmt.Errorf("glob data dir: %w", err)
	}

	// Keyed by player name; IDs are per-capture so name is the stable identity.
	merged := make(map[string]*PlayerEventSummary)

	for _, path := range files {
		players, err := processPlayerEvents(path)
		if err != nil {
			// Skip unreadable/malformed files rather than aborting entirely.
			continue
		}

		for i := range players {
			p := &players[i]
			acc, exists := merged[p.Name]
			if !exists {
				copy := *p
				copy.WeaponStats = make([]PlayerWeaponStat, len(p.WeaponStats))
				_ = copy_weaponStats(copy.WeaponStats, p.WeaponStats)
				merged[p.Name] = &copy
				continue
			}

			acc.KillCount += p.KillCount
			acc.DeathCount += p.DeathCount
			acc.TeamKillCount += p.TeamKillCount

			for _, ws := range p.WeaponStats {
				found := false
				for j := range acc.WeaponStats {
					if acc.WeaponStats[j].Weapon == ws.Weapon {
						acc.WeaponStats[j].Kills += ws.Kills
						found = true
						break
					}
				}
				if !found {
					acc.WeaponStats = append(acc.WeaponStats, ws)
				}
			}
		}
	}

	result := make([]PlayerEventSummary, 0, len(merged))
	for _, p := range merged {
		sort.Slice(p.WeaponStats, func(i, j int) bool {
			return p.WeaponStats[i].Kills > p.WeaponStats[j].Kills
		})
		result = append(result, *p)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].KillCount > result[j].KillCount
	})

	return result, nil
}

// copy_weaponStats copies src into dst and returns the length.
func copy_weaponStats(dst, src []PlayerWeaponStat) int {
	return copy(dst, src)
}

// processPlayerEventsByName iterates every .gz file in dataDir and returns
// a single aggregated PlayerEventSummary for the player matching playerName
// (case-insensitive). Returns nil if no matching player is found.
func processPlayerEventsByName(dataDir, playerName string) (*PlayerEventSummary, error) {
	files, err := filepath.Glob(filepath.Join(dataDir, "*.gz"))
	if err != nil {
		return nil, fmt.Errorf("glob data dir: %w", err)
	}

	normalised := strings.ToLower(playerName)
	var acc *PlayerEventSummary

	for _, path := range files {
		players, err := processPlayerEvents(path)
		if err != nil {
			continue
		}

		for i := range players {
			p := &players[i]
			if strings.ToLower(p.Name) != normalised {
				continue
			}

			if acc == nil {
				copy := *p
				copy.WeaponStats = make([]PlayerWeaponStat, len(p.WeaponStats))
				_ = copy_weaponStats(copy.WeaponStats, p.WeaponStats)
				acc = &copy
				continue
			}

			acc.KillCount += p.KillCount
			acc.DeathCount += p.DeathCount
			acc.TeamKillCount += p.TeamKillCount

			for _, ws := range p.WeaponStats {
				found := false
				for j := range acc.WeaponStats {
					if acc.WeaponStats[j].Weapon == ws.Weapon {
						acc.WeaponStats[j].Kills += ws.Kills
						found = true
						break
					}
				}
				if !found {
					acc.WeaponStats = append(acc.WeaponStats, ws)
				}
			}
		}
	}

	if acc != nil {
		sort.Slice(acc.WeaponStats, func(i, j int) bool {
			return acc.WeaponStats[i].Kills > acc.WeaponStats[j].Kills
		})
	}

	return acc, nil
}

// ---- HTTP handler ----

// GetAllPlayerStats handles GET /api/v1/players
// It aggregates kill/death/weapon statistics for every player across all captures.
func (h *Handler) GetAllPlayerStats(c echo.Context) error {
	players, err := processAllPlayerEvents(h.setting.Data)
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

	player, err := processPlayerEventsByName(h.setting.Data, playerName)
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
