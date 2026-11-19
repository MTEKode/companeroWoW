-- SavedVariables de la forma nueva (schema 2, contrato LEAN): el personaje solo guarda lo minimo y el
-- conocimiento, el mundo y los combates salen por la bandeja "outbox" con su seq. Cada payload es el cuerpo
-- del mensaje de su msgType y lleva "oseq" igual que el item. Lo lee internal/store (ingest_test.go).
CompaneroWoWDB = {
	["schema"] = 2,
	["settings"] = { ["strip"] = true },
	["characters"] = {
		["Fixture-Realm"] = {
			["updatedAt"] = 1795000500,
			["character"] = { ["name"] = "Fixture", ["realm"] = "Realm", ["level"] = 13, ["xp"] = 2100, ["xpMax"] = 4500, ["money"] = 26000 },
		},
	},
	["outbox"] = {
		["seq"] = 4,
		["items"] = {
			{ ["seq"] = 1, ["msgType"] = 9, ["at"] = 1795000400, ["payload"] = {
				["oseq"] = 1,
				["npcs"] = { [900] = { ["npcID"] = 900, ["name"] = "Guardia", ["mapID"] = 1429, ["x"] = 0.5, ["y"] = 0.5, ["isQuestGiver"] = true } },
				["quests"] = { [127] = { ["questID"] = 127, ["title"] = "Mision de prueba", ["level"] = 3, ["giverNpcID"] = 900,
					["giverName"] = "Guardia", ["giverMapID"] = 1429, ["turnInNpcID"] = 901, ["turnInName"] = "Capitan", ["turnInMapID"] = 1429 } },
			} },
			{ ["seq"] = 2, ["msgType"] = 7, ["at"] = 1795000410, ["payload"] = {
				["oseq"] = 2,
				["combat"] = { ["id"] = 7, ["startedAt"] = 1795000410, ["duration"] = 9, ["zone"] = "Dun Morogh", ["died"] = false,
					["damageDone"] = { ["total"] = 210, ["dps"] = 23.3, ["spells"] = { { ["name"] = "Disparo", ["spellID"] = 2764, ["amount"] = 210, ["hits"] = 5 } } },
					["healingDone"] = { ["total"] = 0, ["spells"] = {} },
					["damageTaken"] = { ["total"] = 40, ["sources"] = {} } },
			} },
			{ ["seq"] = 3, ["msgType"] = 16, ["at"] = 1795000420, ["payload"] = {
				["oseq"] = 3,
				["vendors"] = { [901] = { ["npcID"] = 901, ["name"] = "Vendedor de viaje", ["mapID"] = 1426, ["x"] = 0.2, ["y"] = 0.2,
					["items"] = { { ["itemID"] = 4604, ["name"] = "Barra de tiza", ["price"] = 10, ["stack"] = 5 } } } },
			} },
			{ ["seq"] = 4, ["msgType"] = 6, ["at"] = 1795000430, ["payload"] = {
				["oseq"] = 4,
				["add"] = { 127 },
			} },
		},
	},
}
