CompaneroWoWDB = {
	["schema"] = 1,
	["characters"] = {
		["Thrall-Forever"] = {
			["updatedAt"] = 1795000000,
			["character"] = { ["name"] = "Thrall", ["class"] = "Warrior", ["classFile"] = "WARRIOR", ["level"] = 20 },
			["spells"] = {
				{ ["spellID"] = 7384, ["name"] = "Abrumar", ["rank"] = "" },
				{ ["spellID"] = 5308, ["name"] = "Ejecutar", ["rank"] = "" },
				{ ["spellID"] = 78, ["name"] = "Golpe heroico", ["rank"] = "" },
				{ ["spellID"] = 100, ["name"] = "Cargar", ["rank"] = "" },
				{ ["spellID"] = 2457, ["name"] = "Posición de batalla", ["rank"] = "" },
				{ ["spellID"] = 71, ["name"] = "Posición defensiva", ["rank"] = "" },
			},
			["inventory"] = {
				["freeSlots"] = 9,
				["totalSlots"] = 64,
				["items"] = {
					{ ["bag"] = 0, ["slot"] = 1, ["itemID"] = 118, ["name"] = "Poción de curación menor", ["count"] = 5, ["quality"] = 2 },
				},
			},
			["equipment"] = {
				[16] = { ["slotID"] = 16, ["itemID"] = 17070, ["name"] = "Espada de hierro", ["itemLevel"] = 45, ["quality"] = 4 },
			},
			["macros"] = {
				["exportedAt"] = 1795000000,
				["account"] = {
					["used"] = 1, ["max"] = 120,
					["list"] = { { ["index"] = 1, ["name"] = "Global", ["icon"] = 134400, ["body"] = "/cast Ejecutar" } },
				},
				["character"] = {
					["used"] = 4, ["max"] = 18,
					["list"] = {
						{ ["index"] = 121, ["name"] = "Tanque", ["icon"] = 134400, ["body"] = "#showtooltip Abrumar\n/cast [stance:2] Abrumar; Ejecutar" },
						{ ["index"] = 122, ["name"] = "Limpia", ["icon"] = 134400, ["body"] = "/cast Ejecutr" },
						{ ["index"] = 123, ["name"] = "Vieja", ["icon"] = 134400, ["body"] = "/cast [stance:2] Overpower\n/cast Heroic Strike" },
						{ ["index"] = 124, ["name"] = "Clic", ["icon"] = 134400, ["body"] = "/click ActionButton1" },
					},
				},
			},
		},
	},
}
