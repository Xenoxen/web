interface PositionEntry {
	position: number[];
	direction: number;
	alive: number;
	isInVehicle: boolean;
	name: string;
	isPlayer: boolean;
}

interface EntityJSON {
	type: "unit" | "vehicle";
	id: number;
	name: string;
	side: string;
	isPlayer: number;
	startFrameNum: number;
	group: string;
	role: string;
	framesFired: unknown[];
	positions: unknown[][];
}

interface EventJSON {
	0: number;         // frameNum
	1: string;         // type
	2: number;         // victim id
	3: [number, string]; // [causedBy id, weapon]
	4: number;         // distance
}

interface OperationData {
	worldName: string;
	missionName: string;
	endFrame: number;
	captureDelay: number;
	entities: EntityJSON[];
	events: EventJSON[];
	times?: unknown;
	Markers?: unknown[];
}

interface PlayerWeaponStat {
	weapon: string;
	kills: number;
}

interface PlayerEventSummary {
	id: number;
	name: string;
	side: string;
	killCount: number;
	deathCount: number;
	teamKillCount: number;
	weaponStats: PlayerWeaponStat[];
}

async function processPlayerEvents(filepath: string): Promise<PlayerEventSummary[]> {
	const res = await fetch(filepath);
	const data: OperationData = await res.json();

	// Collect only player units
	const playerMap = new Map<number, PlayerEventSummary>();

	for (const entityJSON of data.entities) {
		if (entityJSON.type !== "unit" || entityJSON.isPlayer !== 1) {
			continue;
		}

		// Resolve display name from the last known position name
		let displayName = entityJSON.name;
		if (entityJSON.positions.length > 0) {
			for (let i = entityJSON.positions.length - 1; i >= 0; i--) {
				const entry = entityJSON.positions[i];
				if (Array.isArray(entry) && typeof entry[4] === "string" && entry[4] !== "") {
					displayName = entry[4];
					break;
				}
			}
		}

		playerMap.set(entityJSON.id, {
			id: entityJSON.id,
			name: displayName,
			side: entityJSON.side,
			killCount: 0,
			deathCount: 0,
			teamKillCount: 0,
			weaponStats: [],
		});
	}

	// Process kill/death events
	for (const eventJSON of data.events) {
		const type = eventJSON[1];
		if (type !== "killed" && type !== "hit") {
			continue;
		}

		const victimId = eventJSON[2];
		const causedByInfo = eventJSON[3];
		const causedById = causedByInfo?.[0];
		const weapon = causedByInfo?.[1] ?? "N/A";

		const killer = playerMap.get(causedById);
		const victim = playerMap.get(victimId);

		if (type === "killed") {
			if (killer) {
				killer.killCount++;

				// Track weapon usage
				const existing = killer.weaponStats.find(w => w.weapon === weapon);
				if (existing) {
					existing.kills++;
				} else {
					killer.weaponStats.push({ weapon, kills: 1 });
				}

				// Check for team kill
				const victimSide = victim?.side ?? playerMap.get(victimId)?.side;
				if (victim && killer.id !== victimId && killer.side === victim.side) {
					killer.teamKillCount++;
				}
			}

			if (victim) {
				victim.deathCount++;
			}
		}
	}

	// Sort weapon stats by kills descending
	const players = Array.from(playerMap.values());
	for (const player of players) {
		player.weaponStats.sort((a, b) => b.kills - a.kills);
	}

	return players;
}
