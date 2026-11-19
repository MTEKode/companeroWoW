-- SavedVariables de la forma antigua (schema 1): bloques grandes por personaje y knowledge/world
-- en la raiz. Lo lee internal/store (ingest_test.go) con el evaluador Lua de bridge.
CompaneroWoWDB = {
	["schema"] = 1,
	["settings"] = { ["strip"] = true },
	["maps"] = { [1429] = "Bosque de Elwynn", [1426] = "Dun Morogh" },
	["characters"] = {
		["Fixture-Realm"] = {
			["updatedAt"] = 1795000000,
			["character"] = { ["name"] = "Fixture", ["realm"] = "Realm", ["race"] = "Human", ["class"] = "Warrior", ["level"] = 12, ["xp"] = 1500, ["xpMax"] = 4000, ["money"] = 25000 },
			["quests"] = {
				[1001] = { ["questID"] = 1001, ["title"] = "Lobos del bosque", ["level"] = 4,
					["objectives"] = { { ["text"] = "Lobos muertos", ["numFulfilled"] = 3, ["numRequired"] = 8 } } },
				[1002] = { ["questID"] = 1002, ["title"] = "Entrega simple", ["level"] = 5, ["complete"] = true,
					["objectives"] = {} },
			},
			["completedQuests"] = { 1, 2, 40 },
			["equipment"] = { { ["slot"] = 1, ["itemID"] = 12345 }, { ["slot"] = 16, ["itemID"] = 54321 } },
			["gear"] = {
				["1"] = { ["itemID"] = 12345, ["name"] = "Yelmo de prueba", ["link"] = "|cff0070dd|Hitem:12345::|h[Yelmo de prueba]|h|r",
					["itemLevel"] = 10, ["requiredLevel"] = 8, ["quality"] = 3, ["equipLoc"] = "INVTYPE_HEAD",
					["class"] = "Armor", ["subclass"] = "Mail", ["stats"] = { ["ITEM_MOD_STAMINA_SHORT"] = 9 } },
				["16"] = { ["itemID"] = 54321, ["name"] = "Espada de prueba", ["link"] = "|cff0070dd|Hitem:54321::|h[Espada de prueba]|h|r",
					["itemLevel"] = 11, ["requiredLevel"] = 9, ["quality"] = 2, ["equipLoc"] = "INVTYPE_WEAPONMAINHAND",
					["class"] = "Weapon", ["subclass"] = "Sword", ["weapon"] = { ["min"] = 5, ["max"] = 9, ["speed"] = 2.6 } },
			},
			["combats"] = {
				{ ["id"] = 1, ["startedAt"] = 1795000100, ["duration"] = 12.5, ["zone"] = "Bosque de Elwynn", ["died"] = false,
					["targets"] = { { ["name"] = "Lobo" } },
					["damageDone"] = { ["total"] = 300, ["dps"] = 24, ["spells"] = { { ["name"] = "Golpe heroico", ["spellID"] = 78, ["amount"] = 300, ["hits"] = 3 } } },
					["healingDone"] = { ["total"] = 0, ["spells"] = {} },
					["damageTaken"] = { ["total"] = 80, ["sources"] = { { ["name"] = "Lobo", ["amount"] = 80 } } },
					["resourcesAtEnd"] = { ["healthPct"] = 40 } },
				{ ["id"] = 2, ["startedAt"] = 1795000200, ["duration"] = 30, ["zone"] = "Bosque de Elwynn", ["died"] = true,
					["targets"] = { { ["name"] = "Jabali" } },
					["damageDone"] = { ["total"] = 100, ["dps"] = 3.3, ["spells"] = {} },
					["healingDone"] = { ["total"] = 0, ["spells"] = {} },
					["damageTaken"] = { ["total"] = 500, ["sources"] = { { ["name"] = "Jabali", ["amount"] = 500 } } },
					["death"] = { ["killer"] = "Jabali", ["lastHits"] = { { ["source"] = "Jabali", ["spellName"] = "Mordisco", ["amount"] = 120 } } } },
			},
			["actionBars"] = { ["slots"] = { { ["slot"] = 1, ["kind"] = "spell", ["name"] = "Golpe heroico", ["id"] = 78, ["key"] = "1" } } },
			["macros"] = { ["account"] = { ["list"] = { { ["name"] = "Cuenta", ["body"] = "/say hola", ["index"] = 1 } } },
				["character"] = { ["list"] = { { ["name"] = "Personaje", ["body"] = "/cast Golpe", ["index"] = 2 } } } },
			["session"] = { ["startedAt"] = 1795000000, ["updatedAt"] = 1795000300, ["level"] = { ["start"] = 11, ["now"] = 12 },
				["money"] = { ["start"] = 20000, ["now"] = 25000 }, ["xpGained"] = 500, ["questsCompleted"] = { 1002 }, ["zones"] = { 1429 }, ["deaths"] = 1,
				["previous"] = { { ["startedAt"] = 1794990000, ["endedAt"] = 1794993000, ["levelStart"] = 10, ["levelEnd"] = 11, ["moneyDelta"] = 900, ["xpGained"] = 2000, ["deaths"] = 0 } } },
			["sheet"] = { ["stats"] = { ["stamina"] = 40 } },
		},
	},
	["knowledge"] = {
		["quests"] = {
			[127] = { ["questID"] = 127, ["title"] = "Mision de prueba", ["level"] = 3, ["giverNpcID"] = 900, ["giverName"] = "Guardia", ["giverMapID"] = 1429,
				["giverX"] = 0.5, ["giverY"] = 0.5, ["turnInNpcID"] = 901, ["turnInName"] = "Capitan", ["turnInMapID"] = 1429, ["turnInX"] = 0.4, ["turnInY"] = 0.4,
				["lastSeen"] = 1795000000,
				["rewards"] = { ["money"] = 500, ["xp"] = 300, ["items"] = { { ["itemID"] = 6948, ["link"] = "|Hitem:6948|h[Piedra de hogar]|h", ["count"] = 1, ["choice"] = true } } } },
		},
		["npcs"] = {
			[900] = { ["npcID"] = 900, ["name"] = "Guardia", ["mapID"] = 1429, ["x"] = 0.5, ["y"] = 0.5, ["isQuestGiver"] = true, ["isVendor"] = true, ["lastSeen"] = 1795000000 },
		},
	},
	["world"] = {
		["flight"] = {
			["Alliance"] = {
				["nodes"] = { [10] = { ["nodeID"] = 10, ["name"] = "Ventormenta", ["mapID"] = 1453, ["x"] = 0.5, ["y"] = 0.5 },
					[11] = { ["nodeID"] = 11, ["name"] = "Pueblo Lobo", ["mapID"] = 1429, ["x"] = 0.6, ["y"] = 0.3 } },
				["costs"] = { ["10>11"] = { ["copper"] = 120, ["hops"] = 1, ["seconds"] = 45 } },
			},
			["known"] = { ["Fixture-Realm"] = { 10, 11 } },
		},
		["vendors"] = { [900] = { ["npcID"] = 900, ["name"] = "Guardia", ["mapID"] = 1429, ["x"] = 0.5, ["y"] = 0.5, ["faction"] = "Alliance",
			["items"] = { { ["itemID"] = 2592, ["name"] = "Tela de lino", ["price"] = 50, ["stack"] = 1 } } } },
		["trainers"] = { [950] = { ["npcID"] = 950, ["name"] = "Maestro de armas", ["mapID"] = 1453, ["x"] = 0.3, ["y"] = 0.3, ["kind"] = "class",
			["services"] = { { ["name"] = "Golpe heroico", ["rank"] = "Rango 1", ["cost"] = 100, ["level"] = 3, ["available"] = "yes" } } } },
		["questChains"] = { [1002] = { ["next"] = 1003, ["chainName"] = "Cadena de prueba", ["name"] = "Entrega simple", ["pre"] = { 1001 } } },
		["consumables"] = { [4601] = { ["itemID"] = 4601, ["name"] = "Pan", ["kind"] = "food", ["requiredLevel"] = 1,
			["restores"] = { ["health"] = 100, ["seconds"] = 27 } } },
	},
}
