-- Fixture: copia anonimizada de un CompaneroWoW.lua de esquema 2 (datos reales de un guerrero
-- de nivel 25 y de otros dos personajes). Nombres y reino cambiados; el resto es el dato del addon.
-- Lo leen las pruebas de internal/store, internal/gamedata e internal/tools.
CompaneroWoWDB = {
	["schema"] = 2,
	["characters"] = {
		["Sacerdote-Reino"] = {
			["character"] = {
				["isMaxLevel"] = false,
				["money"] = 43008,
				["class"] = "Sacerdotisa",
				["xp"] = 21364,
				["race"] = "Elfa de la noche",
				["xpMax"] = 29400,
				["restedXP"] = 1262,
				["classFile"] = "PRIEST",
				["faction"] = "Alliance",
				["sex"] = 3,
				["level"] = 23,
				["name"] = "Sacerdote",
				["realm"] = "Reino"
			},
			["updatedAt"] = 1794509786,
			["loggedOutAt"] = 1794509833,
			["combatSeq"] = 428
		},
		["Guerrera-Reino"] = {
			["character"] = {
				["isMaxLevel"] = false,
				["money"] = 100633,
				["class"] = "Guerrera",
				["xp"] = 17662,
				["xpMax"] = 34000,
				["race"] = "Cielonata de la Orden Eminente",
				["name"] = "Guerrera",
				["faction"] = "Alliance",
				["sex"] = 3,
				["level"] = 25,
				["classFile"] = "WARRIOR",
				["realm"] = "Reino"
			},
			["updatedAt"] = 1795175985,
			["loggedOutAt"] = 1795176084,
			["combatSeq"] = 904
		},
		["Chaman-Reino"] = {
			["character"] = {
				["money"] = 166,
				["class"] = "Chamán",
				["xp"] = 3552,
				["race"] = "Enana",
				["xpMax"] = 3600,
				["isMaxLevel"] = false,
				["name"] = "Chaman",
				["faction"] = "Alliance",
				["classFile"] = "SHAMAN",
				["level"] = 6,
				["sex"] = 3,
				["realm"] = "Reino"
			},
			["updatedAt"] = 1794516734,
			["loggedOutAt"] = 1794516755,
			["combatSeq"] = 99
		}
	},
	["outbox"] = {
		["items"] = {
			{
				["at"] = 1795174540,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						887
					},
					["oseq"] = 1227,
					["v"] = {
						["damageDone"] = {
							["total"] = 299,
							["dps"] = 6,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 217
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 82
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174539,
						["duration"] = 50,
						["targets"] = {
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 29,
								["n"] = "Guarda esquelético"
							},
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Asaltante esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795174489,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 226,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 226
								}
							}
						},
						["id"] = 887
					}
				},
				["seq"] = 1227,
				["msgType"] = 7
			},
			{
				["at"] = 1795174590,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						888
					},
					["oseq"] = 1228,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174589,
						["duration"] = 40.1,
						["targets"] = {
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Guarda esquelético"
							},
							{
								["pn"] = 787,
								["classification"] = "normal",
								["lv"] = 26,
								["n"] = "Sanador esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795174548,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 31,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 31
								}
							}
						},
						["id"] = 888
					}
				},
				["seq"] = 1228,
				["msgType"] = 7
			},
			{
				["at"] = 1795174617,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						889
					},
					["oseq"] = 1229,
					["v"] = {
						["damageDone"] = {
							["total"] = 376,
							["dps"] = 15.1,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 320
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 56
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174616,
						["duration"] = 24.9,
						["targets"] = {
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Asaltante esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795174591,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 93,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 93
								}
							}
						},
						["id"] = 889
					}
				},
				["seq"] = 1229,
				["msgType"] = 7
			},
			{
				["at"] = 1795174639,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						890
					},
					["oseq"] = 1230,
					["v"] = {
						["damageDone"] = {
							["total"] = 598,
							["dps"] = 32.7,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 515
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 56
								},
								{
									["spellID"] = 402927,
									["n"] = "Ataque de la victoria",
									["amount"] = 27
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174638,
						["duration"] = 18.3,
						["targets"] = {
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Asaltante esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795174620,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 92,
							["spells"] = {
								{
									["spellID"] = 402927,
									["n"] = "Ataque de la victoria",
									["amount"] = 92
								}
							}
						},
						["damageTaken"] = {
							["total"] = 58,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 58
								}
							}
						},
						["id"] = 890
					}
				},
				["seq"] = 1230,
				["msgType"] = 7
			},
			{
				["at"] = 1795174675,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						891
					},
					["oseq"] = 1231,
					["v"] = {
						["damageDone"] = {
							["total"] = 444,
							["dps"] = 30.2,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 222
								},
								{
									["spellID"] = 7384,
									["n"] = "Abrumar",
									["amount"] = 193
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 29
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174674,
						["duration"] = 14.7,
						["targets"] = {
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Guarda esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795174659,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 33,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 33
								}
							}
						},
						["id"] = 891
					}
				},
				["seq"] = 1231,
				["msgType"] = 7
			},
			{
				["at"] = 1795174717,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						892
					},
					["oseq"] = 1232,
					["v"] = {
						["damageDone"] = {
							["total"] = 1004,
							["dps"] = 30.7,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 716
								},
								{
									["spellID"] = 845,
									["n"] = "Mellar",
									["amount"] = 172
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 88
								},
								{
									["spellID"] = 402927,
									["n"] = "Ataque de la victoria",
									["amount"] = 28
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174716,
						["duration"] = 32.7,
						["targets"] = {
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Guarda esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795174683,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 93,
							["spells"] = {
								{
									["spellID"] = 402927,
									["n"] = "Ataque de la victoria",
									["amount"] = 93
								}
							}
						},
						["damageTaken"] = {
							["total"] = 478,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 478
								}
							}
						},
						["id"] = 892
					}
				},
				["seq"] = 1232,
				["msgType"] = 7
			},
			{
				["at"] = 1795174732,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"consumables",
						4538
					},
					["oseq"] = 1233,
					["v"] = {
						["i"] = 4538,
						["kind"] = "food",
						["n"] = "Sandía Vidrota"
					}
				},
				["seq"] = 1233,
				["msgType"] = 16
			},
			{
				["at"] = 1795174732,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"consumables",
						422
					},
					["oseq"] = 1234,
					["v"] = {
						["i"] = 422,
						["kind"] = "food",
						["n"] = "Cerveza enana"
					}
				},
				["seq"] = 1234,
				["msgType"] = 16
			},
			{
				["at"] = 1795174882,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						893
					},
					["oseq"] = 1235,
					["v"] = {
						["damageDone"] = {
							["total"] = 1099,
							["dps"] = 16.5,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 785
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 112
								},
								{
									["spellID"] = 285,
									["n"] = "Golpe heroico",
									["amount"] = 107
								},
								{
									["spellID"] = 7384,
									["n"] = "Abrumar",
									["amount"] = 95
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174881,
						["duration"] = 66.5,
						["targets"] = {
							{
								["pn"] = 787,
								["classification"] = "normal",
								["lv"] = 27,
								["n"] = "Sanador esquelético"
							},
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 29,
								["n"] = "Guarda esquelético"
							},
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 27,
								["n"] = "Asaltante esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795174814,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 568,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 568
								}
							}
						},
						["id"] = 893
					}
				},
				["seq"] = 1235,
				["msgType"] = 7
			},
			{
				["at"] = 1795174994,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"sheet",
						20777
					},
					["oseq"] = 1236,
					["v"] = {
						["ranged"] = {
							["min"] = 32.57143020629883,
							["speed"] = 1.96,
							["max"] = 48.57143020629883,
							["crit"] = 0.86,
							["ap"] = 109
						},
						["talents"] = {
							["tabs"] = {
								{
									["points"] = 14,
									["talents"] = {
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20504,
											["row"] = 5,
											["n"] = "Intercepción mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12328,
											["row"] = 5,
											["n"] = "Deseo de la Muerte"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 12319,
											["row"] = 6,
											["n"] = "Aluvión"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 23881,
											["row"] = 7,
											["n"] = "Sed de sangre"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 5,
											["spellID"] = 12317,
											["row"] = 3,
											["n"] = "Enfurecer"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20502,
											["row"] = 4,
											["n"] = "Ejecutar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 5,
											["spellID"] = 23584,
											["row"] = 4,
											["n"] = "Especialización en doble empuñadura"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 16487,
											["row"] = 3,
											["n"] = "Manía sangrienta"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12323,
											["row"] = 3,
											["n"] = "Aullido penetrante"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12322,
											["row"] = 2,
											["n"] = "Cólera desbocada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 12321,
											["row"] = 1,
											["n"] = "Voz retumbante"
										},
										{
											["rank"] = 5,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12320,
											["row"] = 1,
											["n"] = "Crueldad"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12294,
											["row"] = 7,
											["n"] = "Golpe mortal"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12289,
											["row"] = 6,
											["n"] = "Seccionar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 5,
											["spellID"] = 1290261,
											["row"] = 5,
											["n"] = "Maestro de armas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12292,
											["row"] = 5,
											["n"] = "Golpes de barrido"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 2,
											["spellID"] = 16493,
											["row"] = 4,
											["n"] = "Clavar"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12163,
											["row"] = 4,
											["n"] = "Especialización en armas de dos manos"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 1,
											["spellID"] = 1310222,
											["row"] = 4,
											["n"] = "Lanzazo"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12834,
											["row"] = 3,
											["n"] = "Heridas profundas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12296,
											["row"] = 3,
											["n"] = "Control de furia"
										},
										{
											["rank"] = 2,
											["col"] = 4,
											["maxRank"] = 2,
											["spellID"] = 12290,
											["row"] = 2,
											["n"] = "Abrumar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 3,
											["spellID"] = 1323963,
											["row"] = 3,
											["n"] = "Precisión furiosa"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12295,
											["row"] = 2,
											["n"] = "Maestría táctica mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12285,
											["row"] = 2,
											["n"] = "Embestir mejorado"
										},
										{
											["rank"] = 3,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12286,
											["row"] = 1,
											["n"] = "Desgarrar mejorado"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 5,
											["spellID"] = 16462,
											["row"] = 1,
											["n"] = "Desvío"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 3,
											["spellID"] = 12282,
											["row"] = 1,
											["n"] = "Golpe heroico mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 23922,
											["row"] = 7,
											["n"] = "Embate con escudo"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 29787,
											["row"] = 5,
											["n"] = "Ira enfocada"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 16538,
											["row"] = 6,
											["n"] = "Bastión"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 2,
											["spellID"] = 12311,
											["row"] = 4,
											["n"] = "Azote de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12312,
											["row"] = 5,
											["n"] = "Muro de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 12809,
											["row"] = 5,
											["n"] = "Golpe de concusión"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 1310317,
											["row"] = 4,
											["n"] = "Vanguardia"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12313,
											["row"] = 3,
											["n"] = "Desarmar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 3,
											["spellID"] = 12308,
											["row"] = 4,
											["n"] = "Hender armadura mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12797,
											["row"] = 2,
											["n"] = "Revancha mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 1,
											["spellID"] = 12975,
											["row"] = 3,
											["n"] = "Última posición"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 2,
											["spellID"] = 1310316,
											["row"] = 3,
											["n"] = "Maestro de defensa"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12287,
											["row"] = 2,
											["n"] = "Trueno mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12301,
											["row"] = 1,
											["n"] = "Sanguinario mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 5,
											["spellID"] = 12297,
											["row"] = 2,
											["n"] = "Anticipación"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 5,
											["spellID"] = 12298,
											["row"] = 1,
											["n"] = "Especialización en escudos"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 1310315,
											["row"] = 6,
											["n"] = "Torbellino mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 20500,
											["row"] = 5,
											["n"] = "Ira rabiosa mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 1289682,
											["row"] = 5,
											["n"] = "Fragor de la sangre"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12792,
											["row"] = 3,
											["n"] = "Desafío"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 1323964,
											["row"] = 2,
											["n"] = "Ira persistente"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12862,
											["row"] = 6,
											["n"] = "Embate mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 2,
											["spellID"] = 1323967,
											["row"] = 5,
											["n"] = "Bebedor de entrañas"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 12962,
											["row"] = 1,
											["n"] = "Voluntad férrea"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 1329555,
											["row"] = 4,
											["n"] = "Golpes temerarios"
										}
									}
								}
							}
						},
						["class"] = "WARRIOR",
						["race"] = "Skyborne",
						["armor"] = 16,
						["lv"] = 25,
						["base"] = {
							["str"] = 73,
							["sta"] = 76,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["total"] = {
							["str"] = 73,
							["sta"] = 76,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["melee"] = {
							["mhMin"] = 95.81428527832031,
							["attackSpeed"] = 3.33,
							["apPos"] = 0,
							["expertise"] = 0,
							["mhMax"] = 120.8142852783203,
							["apBase"] = 201,
							["hit"] = 0,
							["apNeg"] = 0,
							["mhSpeed"] = 3.33,
							["crit"] = 10.74,
							["ap"] = 201
						},
						["spells"] = {
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 6603,
								["range"] = 0,
								["n"] = "Atacar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0,
								["spellID"] = 1259416,
								["range"] = 0,
								["n"] = "Caminar sobre el aire"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0.98,
								["spellID"] = 2480,
								["range"] = 30,
								["n"] = "Disparar arco"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2764,
								["range"] = 30,
								["n"] = "Lanzar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0.49,
								["spellID"] = 1259705,
								["range"] = 0,
								["n"] = "Read Leyline"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259710,
								["range"] = 0,
								["n"] = "Bendición del viento"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 107,
								["range"] = 0,
								["n"] = "Bloqueo"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293712,
								["range"] = 0,
								["n"] = "Competencia con armaduras"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 674,
								["range"] = 0,
								["n"] = "Doble empuñadura"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 81,
								["range"] = 0,
								["n"] = "Esquivar"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293657,
								["range"] = 0,
								["n"] = "Idiomas"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 3127,
								["range"] = 0,
								["n"] = "Parar"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259707,
								["range"] = 0,
								["n"] = "Perspicacia elemental"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Abrumar",
								["range"] = 0,
								["spellID"] = 7384
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud de batalla",
								["range"] = 0,
								["spellID"] = 2457
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 402927,
								["range"] = 0,
								["n"] = "Ataque de la victoria"
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Desgarrar",
								["range"] = 0,
								["spellID"] = 6547
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Embestir",
								["range"] = 25,
								["spellID"] = 100
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Falsa arremetida",
								["range"] = 0,
								["spellID"] = 694
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Golpe heroico",
								["range"] = 0,
								["spellID"] = 285
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Represalias",
								["range"] = 0,
								["spellID"] = 20230
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Seccionar",
								["range"] = 0,
								["spellID"] = 1715
							},
							{
								["rank"] = "Rango 2",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Trueno",
								["range"] = 0,
								["spellID"] = 8198
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1310185,
								["range"] = 0,
								["n"] = "Maestría táctica"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 1.5,
								["n"] = "Embate",
								["range"] = 0,
								["spellID"] = 1240193
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito de guerra",
								["range"] = 0,
								["spellID"] = 6192
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito desmoralizador",
								["range"] = 0,
								["spellID"] = 1160
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Mellar",
								["range"] = 0,
								["spellID"] = 845
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud defensiva",
								["range"] = 0,
								["spellID"] = 71
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Azote de escudo",
								["range"] = 0,
								["spellID"] = 72
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Bloquear con escudo",
								["range"] = 0,
								["spellID"] = 2565
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Desactivar",
								["range"] = 0,
								["spellID"] = 676
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Hender armadura",
								["range"] = 0,
								["spellID"] = 7386
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Irritar",
								["range"] = 0,
								["spellID"] = 355
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Revancha",
								["range"] = 0,
								["spellID"] = 6572
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2687,
								["range"] = 0,
								["n"] = "Sanguinario"
							}
						},
						["regen"] = {
							["manaBase"] = 0,
							["manaCasting"] = 0
						},
						["resist"] = {
							["holy"] = 0,
							["arcane"] = 0,
							["shadow"] = 0,
							["fire"] = 0,
							["frost"] = 0,
							["nature"] = 0
						},
						["powerMax"] = 100,
						["armorEffective"] = 846,
						["defense"] = {
							["parry"] = 6.92,
							["block"] = 4.92,
							["blockValue"] = 2,
							["dodge"] = 5.7
						},
						["spell"] = {
							["power"] = 7,
							["haste"] = 0,
							["crit"] = 0,
							["hit"] = 0
						},
						["healthMax"] = 854,
						["powerType"] = "RAGE",
						["stance"] = "Actitud de batalla"
					}
				},
				["seq"] = 1236,
				["msgType"] = 12
			},
			{
				["at"] = 1795174994,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						894
					},
					["oseq"] = 1237,
					["v"] = {
						["damageDone"] = {
							["total"] = 1001,
							["dps"] = 16.2,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 765
								},
								{
									["spellID"] = 285,
									["n"] = "Golpe heroico",
									["amount"] = 98
								},
								{
									["spellID"] = 7384,
									["n"] = "Abrumar",
									["amount"] = 83
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 55
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795174993,
						["duration"] = 61.7,
						["targets"] = {
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Asaltante esquelético"
							}
						},
						["died"] = true,
						["inInstance"] = false,
						["startedAt"] = 1795174932,
						["death"] = {
							["killer"] = "",
							["lastHits"] = {
								{
									["spellName"] = "Atacar",
									["source"] = "",
									["amount"] = 893
								}
							}
						},
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 893,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 893
								}
							}
						},
						["id"] = 894
					}
				},
				["seq"] = 1237,
				["msgType"] = 7
			},
			{
				["at"] = 1795175158,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						895
					},
					["oseq"] = 1238,
					["v"] = {
						["damageDone"] = {
							["total"] = 261,
							["dps"] = 39.5,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 235
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 26
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175157,
						["duration"] = 6.6,
						["targets"] = {
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 27,
								["n"] = "Asaltante esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795175150,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 0,
							["sources"] = {}
						},
						["id"] = 895
					}
				},
				["seq"] = 1238,
				["msgType"] = 7
			},
			{
				["at"] = 1795175221,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						896
					},
					["oseq"] = 1239,
					["v"] = {
						["damageDone"] = {
							["total"] = 441,
							["dps"] = 16.3,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 412
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 29
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175220,
						["duration"] = 27,
						["targets"] = {
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 27,
								["n"] = "Asaltante esquelético"
							},
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 28,
								["n"] = "Guarda esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795175193,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 85,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 85
								}
							}
						},
						["id"] = 896
					}
				},
				["seq"] = 1239,
				["msgType"] = 7
			},
			{
				["at"] = 1795175257,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"sheet",
						20777
					},
					["oseq"] = 1240,
					["v"] = {
						["ranged"] = {
							["min"] = 29.31428718566895,
							["speed"] = 1.96,
							["max"] = 43.71428680419922,
							["crit"] = 0.86,
							["ap"] = 109
						},
						["talents"] = {
							["tabs"] = {
								{
									["points"] = 14,
									["talents"] = {
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20504,
											["row"] = 5,
											["n"] = "Intercepción mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12328,
											["row"] = 5,
											["n"] = "Deseo de la Muerte"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 12319,
											["row"] = 6,
											["n"] = "Aluvión"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 23881,
											["row"] = 7,
											["n"] = "Sed de sangre"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 5,
											["spellID"] = 12317,
											["row"] = 3,
											["n"] = "Enfurecer"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20502,
											["row"] = 4,
											["n"] = "Ejecutar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 5,
											["spellID"] = 23584,
											["row"] = 4,
											["n"] = "Especialización en doble empuñadura"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 16487,
											["row"] = 3,
											["n"] = "Manía sangrienta"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12323,
											["row"] = 3,
											["n"] = "Aullido penetrante"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12322,
											["row"] = 2,
											["n"] = "Cólera desbocada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 12321,
											["row"] = 1,
											["n"] = "Voz retumbante"
										},
										{
											["rank"] = 5,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12320,
											["row"] = 1,
											["n"] = "Crueldad"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12294,
											["row"] = 7,
											["n"] = "Golpe mortal"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12289,
											["row"] = 6,
											["n"] = "Seccionar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 5,
											["spellID"] = 1290261,
											["row"] = 5,
											["n"] = "Maestro de armas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12292,
											["row"] = 5,
											["n"] = "Golpes de barrido"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 2,
											["spellID"] = 16493,
											["row"] = 4,
											["n"] = "Clavar"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12163,
											["row"] = 4,
											["n"] = "Especialización en armas de dos manos"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 1,
											["spellID"] = 1310222,
											["row"] = 4,
											["n"] = "Lanzazo"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12834,
											["row"] = 3,
											["n"] = "Heridas profundas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12296,
											["row"] = 3,
											["n"] = "Control de furia"
										},
										{
											["rank"] = 2,
											["col"] = 4,
											["maxRank"] = 2,
											["spellID"] = 12290,
											["row"] = 2,
											["n"] = "Abrumar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 3,
											["spellID"] = 1323963,
											["row"] = 3,
											["n"] = "Precisión furiosa"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12295,
											["row"] = 2,
											["n"] = "Maestría táctica mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12285,
											["row"] = 2,
											["n"] = "Embestir mejorado"
										},
										{
											["rank"] = 3,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12286,
											["row"] = 1,
											["n"] = "Desgarrar mejorado"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 5,
											["spellID"] = 16462,
											["row"] = 1,
											["n"] = "Desvío"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 3,
											["spellID"] = 12282,
											["row"] = 1,
											["n"] = "Golpe heroico mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 23922,
											["row"] = 7,
											["n"] = "Embate con escudo"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 29787,
											["row"] = 5,
											["n"] = "Ira enfocada"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 16538,
											["row"] = 6,
											["n"] = "Bastión"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 2,
											["spellID"] = 12311,
											["row"] = 4,
											["n"] = "Azote de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12312,
											["row"] = 5,
											["n"] = "Muro de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 12809,
											["row"] = 5,
											["n"] = "Golpe de concusión"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 1310317,
											["row"] = 4,
											["n"] = "Vanguardia"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12313,
											["row"] = 3,
											["n"] = "Desarmar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 3,
											["spellID"] = 12308,
											["row"] = 4,
											["n"] = "Hender armadura mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12797,
											["row"] = 2,
											["n"] = "Revancha mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 1,
											["spellID"] = 12975,
											["row"] = 3,
											["n"] = "Última posición"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 2,
											["spellID"] = 1310316,
											["row"] = 3,
											["n"] = "Maestro de defensa"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12287,
											["row"] = 2,
											["n"] = "Trueno mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12301,
											["row"] = 1,
											["n"] = "Sanguinario mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 5,
											["spellID"] = 12297,
											["row"] = 2,
											["n"] = "Anticipación"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 5,
											["spellID"] = 12298,
											["row"] = 1,
											["n"] = "Especialización en escudos"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 1310315,
											["row"] = 6,
											["n"] = "Torbellino mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 20500,
											["row"] = 5,
											["n"] = "Ira rabiosa mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 1289682,
											["row"] = 5,
											["n"] = "Fragor de la sangre"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12792,
											["row"] = 3,
											["n"] = "Desafío"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 1323964,
											["row"] = 2,
											["n"] = "Ira persistente"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12862,
											["row"] = 6,
											["n"] = "Embate mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 2,
											["spellID"] = 1323967,
											["row"] = 5,
											["n"] = "Bebedor de entrañas"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 12962,
											["row"] = 1,
											["n"] = "Voluntad férrea"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 1329555,
											["row"] = 4,
											["n"] = "Golpes temerarios"
										}
									}
								}
							}
						},
						["class"] = "WARRIOR",
						["healthMax"] = 794,
						["armor"] = 16,
						["lv"] = 25,
						["base"] = {
							["str"] = 73,
							["sta"] = 70,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["spell"] = {
							["power"] = 0,
							["haste"] = 0,
							["crit"] = 0,
							["hit"] = 0
						},
						["melee"] = {
							["mhMin"] = 30.98571968078613,
							["attackSpeed"] = 1.96,
							["apPos"] = 33,
							["expertise"] = 0,
							["mhMax"] = 30.98571968078613,
							["apBase"] = 201,
							["hit"] = 0,
							["apNeg"] = 0,
							["mhSpeed"] = 1.96,
							["crit"] = 0,
							["ap"] = 234
						},
						["spells"] = {
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 6603,
								["range"] = 0,
								["n"] = "Atacar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0,
								["spellID"] = 1259416,
								["range"] = 0,
								["n"] = "Caminar sobre el aire"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0.98,
								["spellID"] = 2480,
								["range"] = 30,
								["n"] = "Disparar arco"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2764,
								["range"] = 30,
								["n"] = "Lanzar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0.49,
								["spellID"] = 1259705,
								["range"] = 0,
								["n"] = "Read Leyline"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259710,
								["range"] = 0,
								["n"] = "Bendición del viento"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 107,
								["range"] = 0,
								["n"] = "Bloqueo"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293712,
								["range"] = 0,
								["n"] = "Competencia con armaduras"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 674,
								["range"] = 0,
								["n"] = "Doble empuñadura"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 81,
								["range"] = 0,
								["n"] = "Esquivar"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293657,
								["range"] = 0,
								["n"] = "Idiomas"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 3127,
								["range"] = 0,
								["n"] = "Parar"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259707,
								["range"] = 0,
								["n"] = "Perspicacia elemental"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Abrumar",
								["range"] = 0,
								["spellID"] = 7384
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud de batalla",
								["range"] = 0,
								["spellID"] = 2457
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 402927,
								["range"] = 0,
								["n"] = "Ataque de la victoria"
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Desgarrar",
								["range"] = 0,
								["spellID"] = 6547
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Embestir",
								["range"] = 25,
								["spellID"] = 100
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Falsa arremetida",
								["range"] = 0,
								["spellID"] = 694
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Golpe heroico",
								["range"] = 0,
								["spellID"] = 285
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Represalias",
								["range"] = 0,
								["spellID"] = 20230
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Seccionar",
								["range"] = 0,
								["spellID"] = 1715
							},
							{
								["rank"] = "Rango 2",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Trueno",
								["range"] = 0,
								["spellID"] = 8198
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1310185,
								["range"] = 0,
								["n"] = "Maestría táctica"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 1.5,
								["n"] = "Embate",
								["range"] = 0,
								["spellID"] = 1240193
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito de guerra",
								["range"] = 0,
								["spellID"] = 6192
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito desmoralizador",
								["range"] = 0,
								["spellID"] = 1160
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Mellar",
								["range"] = 0,
								["spellID"] = 845
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud defensiva",
								["range"] = 0,
								["spellID"] = 71
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Azote de escudo",
								["range"] = 0,
								["spellID"] = 72
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Bloquear con escudo",
								["range"] = 0,
								["spellID"] = 2565
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Desactivar",
								["range"] = 0,
								["spellID"] = 676
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Hender armadura",
								["range"] = 0,
								["spellID"] = 7386
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Irritar",
								["range"] = 0,
								["spellID"] = 355
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Revancha",
								["range"] = 0,
								["spellID"] = 6572
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2687,
								["range"] = 0,
								["n"] = "Sanguinario"
							}
						},
						["regen"] = {
							["manaBase"] = 0,
							["manaCasting"] = 0
						},
						["armorEffective"] = 1274,
						["resist"] = {
							["holy"] = 0,
							["arcane"] = 0,
							["shadow"] = 0,
							["fire"] = 0,
							["frost"] = 0,
							["nature"] = 4
						},
						["powerMax"] = 100,
						["total"] = {
							["str"] = 73,
							["sta"] = 70,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["defense"] = {
							["parry"] = 6.92,
							["block"] = 4.92,
							["blockValue"] = 9,
							["dodge"] = 5.7
						},
						["race"] = "Skyborne",
						["buffs"] = {
							6192
						},
						["powerType"] = "RAGE",
						["stance"] = "Actitud defensiva"
					}
				},
				["seq"] = 1240,
				["msgType"] = 12
			},
			{
				["at"] = 1795175258,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						897
					},
					["oseq"] = 1241,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175257,
						["duration"] = 30.8,
						["targets"] = {
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 29,
								["n"] = "Guarda esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795175226,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 26,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 26
								}
							}
						},
						["id"] = 897
					}
				},
				["seq"] = 1241,
				["msgType"] = 7
			},
			{
				["at"] = 1795175259,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						898
					},
					["oseq"] = 1242,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175258,
						["duration"] = 0.4,
						["targets"] = {
							{
								["pn"] = 785,
								["classification"] = "normal",
								["lv"] = 29,
								["n"] = "Guarda esquelético"
							}
						},
						["died"] = true,
						["inInstance"] = false,
						["startedAt"] = 1795175258,
						["death"] = {
							["killer"] = "",
							["lastHits"] = {
								{
									["spellName"] = "Atacar",
									["source"] = "",
									["amount"] = 101
								},
								{
									["spellName"] = "Atacar",
									["source"] = "",
									["amount"] = 26
								}
							}
						},
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 127,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 127
								}
							}
						},
						["id"] = 898
					}
				},
				["seq"] = 1242,
				["msgType"] = 7
			},
			{
				["at"] = 1795175315,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"sheet",
						20777
					},
					["oseq"] = 1243,
					["v"] = {
						["ranged"] = {
							["min"] = 32.57143020629883,
							["speed"] = 1.96,
							["max"] = 48.57143020629883,
							["crit"] = 0.86,
							["ap"] = 109
						},
						["talents"] = {
							["tabs"] = {
								{
									["points"] = 14,
									["talents"] = {
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20504,
											["row"] = 5,
											["n"] = "Intercepción mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12328,
											["row"] = 5,
											["n"] = "Deseo de la Muerte"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 12319,
											["row"] = 6,
											["n"] = "Aluvión"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 23881,
											["row"] = 7,
											["n"] = "Sed de sangre"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 5,
											["spellID"] = 12317,
											["row"] = 3,
											["n"] = "Enfurecer"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20502,
											["row"] = 4,
											["n"] = "Ejecutar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 5,
											["spellID"] = 23584,
											["row"] = 4,
											["n"] = "Especialización en doble empuñadura"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 16487,
											["row"] = 3,
											["n"] = "Manía sangrienta"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12323,
											["row"] = 3,
											["n"] = "Aullido penetrante"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12322,
											["row"] = 2,
											["n"] = "Cólera desbocada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 12321,
											["row"] = 1,
											["n"] = "Voz retumbante"
										},
										{
											["rank"] = 5,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12320,
											["row"] = 1,
											["n"] = "Crueldad"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12294,
											["row"] = 7,
											["n"] = "Golpe mortal"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12289,
											["row"] = 6,
											["n"] = "Seccionar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 5,
											["spellID"] = 1290261,
											["row"] = 5,
											["n"] = "Maestro de armas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12292,
											["row"] = 5,
											["n"] = "Golpes de barrido"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 2,
											["spellID"] = 16493,
											["row"] = 4,
											["n"] = "Clavar"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12163,
											["row"] = 4,
											["n"] = "Especialización en armas de dos manos"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 1,
											["spellID"] = 1310222,
											["row"] = 4,
											["n"] = "Lanzazo"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12834,
											["row"] = 3,
											["n"] = "Heridas profundas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12296,
											["row"] = 3,
											["n"] = "Control de furia"
										},
										{
											["rank"] = 2,
											["col"] = 4,
											["maxRank"] = 2,
											["spellID"] = 12290,
											["row"] = 2,
											["n"] = "Abrumar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 3,
											["spellID"] = 1323963,
											["row"] = 3,
											["n"] = "Precisión furiosa"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12295,
											["row"] = 2,
											["n"] = "Maestría táctica mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12285,
											["row"] = 2,
											["n"] = "Embestir mejorado"
										},
										{
											["rank"] = 3,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12286,
											["row"] = 1,
											["n"] = "Desgarrar mejorado"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 5,
											["spellID"] = 16462,
											["row"] = 1,
											["n"] = "Desvío"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 3,
											["spellID"] = 12282,
											["row"] = 1,
											["n"] = "Golpe heroico mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 23922,
											["row"] = 7,
											["n"] = "Embate con escudo"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 29787,
											["row"] = 5,
											["n"] = "Ira enfocada"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 16538,
											["row"] = 6,
											["n"] = "Bastión"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 2,
											["spellID"] = 12311,
											["row"] = 4,
											["n"] = "Azote de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12312,
											["row"] = 5,
											["n"] = "Muro de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 12809,
											["row"] = 5,
											["n"] = "Golpe de concusión"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 1310317,
											["row"] = 4,
											["n"] = "Vanguardia"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12313,
											["row"] = 3,
											["n"] = "Desarmar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 3,
											["spellID"] = 12308,
											["row"] = 4,
											["n"] = "Hender armadura mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12797,
											["row"] = 2,
											["n"] = "Revancha mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 1,
											["spellID"] = 12975,
											["row"] = 3,
											["n"] = "Última posición"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 2,
											["spellID"] = 1310316,
											["row"] = 3,
											["n"] = "Maestro de defensa"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12287,
											["row"] = 2,
											["n"] = "Trueno mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12301,
											["row"] = 1,
											["n"] = "Sanguinario mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 5,
											["spellID"] = 12297,
											["row"] = 2,
											["n"] = "Anticipación"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 5,
											["spellID"] = 12298,
											["row"] = 1,
											["n"] = "Especialización en escudos"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 1310315,
											["row"] = 6,
											["n"] = "Torbellino mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 20500,
											["row"] = 5,
											["n"] = "Ira rabiosa mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 1289682,
											["row"] = 5,
											["n"] = "Fragor de la sangre"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12792,
											["row"] = 3,
											["n"] = "Desafío"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 1323964,
											["row"] = 2,
											["n"] = "Ira persistente"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12862,
											["row"] = 6,
											["n"] = "Embate mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 2,
											["spellID"] = 1323967,
											["row"] = 5,
											["n"] = "Bebedor de entrañas"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 12962,
											["row"] = 1,
											["n"] = "Voluntad férrea"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 1329555,
											["row"] = 4,
											["n"] = "Golpes temerarios"
										}
									}
								}
							}
						},
						["class"] = "WARRIOR",
						["race"] = "Skyborne",
						["armor"] = 16,
						["lv"] = 25,
						["base"] = {
							["str"] = 73,
							["sta"] = 76,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["total"] = {
							["str"] = 73,
							["sta"] = 76,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["melee"] = {
							["mhMin"] = 95.81428527832031,
							["attackSpeed"] = 3.33,
							["apPos"] = 0,
							["expertise"] = 0,
							["mhMax"] = 120.8142852783203,
							["apBase"] = 201,
							["hit"] = 0,
							["apNeg"] = 0,
							["mhSpeed"] = 3.33,
							["crit"] = 10.74,
							["ap"] = 201
						},
						["spells"] = {
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 6603,
								["range"] = 0,
								["n"] = "Atacar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0,
								["spellID"] = 1259416,
								["range"] = 0,
								["n"] = "Caminar sobre el aire"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0.98,
								["spellID"] = 2480,
								["range"] = 30,
								["n"] = "Disparar arco"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2764,
								["range"] = 30,
								["n"] = "Lanzar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0.49,
								["spellID"] = 1259705,
								["range"] = 0,
								["n"] = "Read Leyline"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259710,
								["range"] = 0,
								["n"] = "Bendición del viento"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 107,
								["range"] = 0,
								["n"] = "Bloqueo"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293712,
								["range"] = 0,
								["n"] = "Competencia con armaduras"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 674,
								["range"] = 0,
								["n"] = "Doble empuñadura"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 81,
								["range"] = 0,
								["n"] = "Esquivar"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293657,
								["range"] = 0,
								["n"] = "Idiomas"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 3127,
								["range"] = 0,
								["n"] = "Parar"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259707,
								["range"] = 0,
								["n"] = "Perspicacia elemental"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Abrumar",
								["range"] = 0,
								["spellID"] = 7384
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud de batalla",
								["range"] = 0,
								["spellID"] = 2457
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 402927,
								["range"] = 0,
								["n"] = "Ataque de la victoria"
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Desgarrar",
								["range"] = 0,
								["spellID"] = 6547
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Embestir",
								["range"] = 25,
								["spellID"] = 100
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Falsa arremetida",
								["range"] = 0,
								["spellID"] = 694
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Golpe heroico",
								["range"] = 0,
								["spellID"] = 285
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Represalias",
								["range"] = 0,
								["spellID"] = 20230
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Seccionar",
								["range"] = 0,
								["spellID"] = 1715
							},
							{
								["rank"] = "Rango 2",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Trueno",
								["range"] = 0,
								["spellID"] = 8198
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1310185,
								["range"] = 0,
								["n"] = "Maestría táctica"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 1.5,
								["n"] = "Embate",
								["range"] = 0,
								["spellID"] = 1240193
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito de guerra",
								["range"] = 0,
								["spellID"] = 6192
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito desmoralizador",
								["range"] = 0,
								["spellID"] = 1160
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Mellar",
								["range"] = 0,
								["spellID"] = 845
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud defensiva",
								["range"] = 0,
								["spellID"] = 71
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Azote de escudo",
								["range"] = 0,
								["spellID"] = 72
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Bloquear con escudo",
								["range"] = 0,
								["spellID"] = 2565
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Desactivar",
								["range"] = 0,
								["spellID"] = 676
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Hender armadura",
								["range"] = 0,
								["spellID"] = 7386
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Irritar",
								["range"] = 0,
								["spellID"] = 355
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Revancha",
								["range"] = 0,
								["spellID"] = 6572
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2687,
								["range"] = 0,
								["n"] = "Sanguinario"
							}
						},
						["regen"] = {
							["manaBase"] = 0,
							["manaCasting"] = 0
						},
						["resist"] = {
							["holy"] = 0,
							["arcane"] = 0,
							["shadow"] = 0,
							["fire"] = 0,
							["frost"] = 0,
							["nature"] = 0
						},
						["powerMax"] = 100,
						["armorEffective"] = 846,
						["defense"] = {
							["parry"] = 6.92,
							["block"] = 4.92,
							["blockValue"] = 2,
							["dodge"] = 5.7
						},
						["spell"] = {
							["power"] = 7,
							["haste"] = 0,
							["crit"] = 0,
							["hit"] = 0
						},
						["healthMax"] = 854,
						["powerType"] = "RAGE",
						["stance"] = "Actitud de batalla"
					}
				},
				["seq"] = 1243,
				["msgType"] = 12
			},
			{
				["at"] = 1795175316,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						899
					},
					["oseq"] = 1244,
					["v"] = {
						["damageDone"] = {
							["total"] = 226,
							["dps"] = 12.2,
							["spells"] = {
								{
									["spellID"] = 6603,
									["n"] = "Atacar",
									["amount"] = 198
								},
								{
									["spellID"] = 8198,
									["n"] = "Trueno",
									["amount"] = 28
								}
							}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175315,
						["duration"] = 18.6,
						["targets"] = {
							{
								["pn"] = 1110,
								["classification"] = "normal",
								["lv"] = 27,
								["n"] = "Asaltante esquelético"
							}
						},
						["died"] = true,
						["inInstance"] = false,
						["startedAt"] = 1795175296,
						["death"] = {
							["killer"] = "",
							["lastHits"] = {
								{
									["spellName"] = "Atacar",
									["source"] = "",
									["amount"] = 512
								},
								{
									["spellName"] = "Atacar",
									["source"] = "",
									["amount"] = 79
								},
								{
									["spellName"] = "Atacar",
									["source"] = "",
									["amount"] = 26
								}
							}
						},
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 617,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 617
								}
							}
						},
						["id"] = 899
					}
				},
				["seq"] = 1244,
				["msgType"] = 7
			},
			{
				["at"] = 1795175545,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"sheet",
						20777
					},
					["oseq"] = 1245,
					["v"] = {
						["ranged"] = {
							["min"] = 29.31428718566895,
							["speed"] = 1.96,
							["max"] = 43.71428680419922,
							["crit"] = 0.86,
							["ap"] = 109
						},
						["talents"] = {
							["tabs"] = {
								{
									["points"] = 14,
									["talents"] = {
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20504,
											["row"] = 5,
											["n"] = "Intercepción mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12328,
											["row"] = 5,
											["n"] = "Deseo de la Muerte"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 12319,
											["row"] = 6,
											["n"] = "Aluvión"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 23881,
											["row"] = 7,
											["n"] = "Sed de sangre"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 5,
											["spellID"] = 12317,
											["row"] = 3,
											["n"] = "Enfurecer"
										},
										{
											["rank"] = 0,
											["col"] = 8,
											["maxRank"] = 2,
											["spellID"] = 20502,
											["row"] = 4,
											["n"] = "Ejecutar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 5,
											["spellID"] = 23584,
											["row"] = 4,
											["n"] = "Especialización en doble empuñadura"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 3,
											["spellID"] = 16487,
											["row"] = 3,
											["n"] = "Manía sangrienta"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 12323,
											["row"] = 3,
											["n"] = "Aullido penetrante"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12322,
											["row"] = 2,
											["n"] = "Cólera desbocada"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 12321,
											["row"] = 1,
											["n"] = "Voz retumbante"
										},
										{
											["rank"] = 5,
											["col"] = 7,
											["maxRank"] = 5,
											["spellID"] = 12320,
											["row"] = 1,
											["n"] = "Crueldad"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12294,
											["row"] = 7,
											["n"] = "Golpe mortal"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12289,
											["row"] = 6,
											["n"] = "Seccionar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 5,
											["spellID"] = 1290261,
											["row"] = 5,
											["n"] = "Maestro de armas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12292,
											["row"] = 5,
											["n"] = "Golpes de barrido"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 2,
											["spellID"] = 16493,
											["row"] = 4,
											["n"] = "Clavar"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12163,
											["row"] = 4,
											["n"] = "Especialización en armas de dos manos"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 1,
											["spellID"] = 1310222,
											["row"] = 4,
											["n"] = "Lanzazo"
										},
										{
											["rank"] = 0,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12834,
											["row"] = 3,
											["n"] = "Heridas profundas"
										},
										{
											["rank"] = 0,
											["col"] = 2,
											["maxRank"] = 1,
											["spellID"] = 12296,
											["row"] = 3,
											["n"] = "Control de furia"
										},
										{
											["rank"] = 2,
											["col"] = 4,
											["maxRank"] = 2,
											["spellID"] = 12290,
											["row"] = 2,
											["n"] = "Abrumar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 3,
											["spellID"] = 1323963,
											["row"] = 3,
											["n"] = "Precisión furiosa"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 3,
											["spellID"] = 12295,
											["row"] = 2,
											["n"] = "Maestría táctica mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12285,
											["row"] = 2,
											["n"] = "Embestir mejorado"
										},
										{
											["rank"] = 3,
											["col"] = 3,
											["maxRank"] = 3,
											["spellID"] = 12286,
											["row"] = 1,
											["n"] = "Desgarrar mejorado"
										},
										{
											["rank"] = 2,
											["col"] = 2,
											["maxRank"] = 5,
											["spellID"] = 16462,
											["row"] = 1,
											["n"] = "Desvío"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 3,
											["spellID"] = 12282,
											["row"] = 1,
											["n"] = "Golpe heroico mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 23922,
											["row"] = 7,
											["n"] = "Embate con escudo"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 29787,
											["row"] = 5,
											["n"] = "Ira enfocada"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 16538,
											["row"] = 6,
											["n"] = "Bastión"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 2,
											["spellID"] = 12311,
											["row"] = 4,
											["n"] = "Azote de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12312,
											["row"] = 5,
											["n"] = "Muro de escudo mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 12809,
											["row"] = 5,
											["n"] = "Golpe de concusión"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 1,
											["spellID"] = 1310317,
											["row"] = 4,
											["n"] = "Vanguardia"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12313,
											["row"] = 3,
											["n"] = "Desarmar mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 3,
											["spellID"] = 12308,
											["row"] = 4,
											["n"] = "Hender armadura mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 3,
											["spellID"] = 12797,
											["row"] = 2,
											["n"] = "Revancha mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 1,
											["spellID"] = 12975,
											["row"] = 3,
											["n"] = "Última posición"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 2,
											["spellID"] = 1310316,
											["row"] = 3,
											["n"] = "Maestro de defensa"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12287,
											["row"] = 2,
											["n"] = "Trueno mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 2,
											["spellID"] = 12301,
											["row"] = 1,
											["n"] = "Sanguinario mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 9,
											["maxRank"] = 5,
											["spellID"] = 12297,
											["row"] = 2,
											["n"] = "Anticipación"
										},
										{
											["rank"] = 0,
											["col"] = 10,
											["maxRank"] = 5,
											["spellID"] = 12298,
											["row"] = 1,
											["n"] = "Especialización en escudos"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 1310315,
											["row"] = 6,
											["n"] = "Torbellino mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 5,
											["maxRank"] = 2,
											["spellID"] = 20500,
											["row"] = 5,
											["n"] = "Ira rabiosa mejorada"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 1289682,
											["row"] = 5,
											["n"] = "Fragor de la sangre"
										},
										{
											["rank"] = 0,
											["col"] = 12,
											["maxRank"] = 3,
											["spellID"] = 12792,
											["row"] = 3,
											["n"] = "Desafío"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 5,
											["spellID"] = 1323964,
											["row"] = 2,
											["n"] = "Ira persistente"
										},
										{
											["rank"] = 0,
											["col"] = 1,
											["maxRank"] = 2,
											["spellID"] = 12862,
											["row"] = 6,
											["n"] = "Embate mejorado"
										},
										{
											["rank"] = 0,
											["col"] = 7,
											["maxRank"] = 2,
											["spellID"] = 1323967,
											["row"] = 5,
											["n"] = "Bebedor de entrañas"
										},
										{
											["rank"] = 0,
											["col"] = 11,
											["maxRank"] = 5,
											["spellID"] = 12962,
											["row"] = 1,
											["n"] = "Voluntad férrea"
										},
										{
											["rank"] = 0,
											["col"] = 6,
											["maxRank"] = 1,
											["spellID"] = 1329555,
											["row"] = 4,
											["n"] = "Golpes temerarios"
										}
									}
								}
							}
						},
						["class"] = "WARRIOR",
						["race"] = "Skyborne",
						["armor"] = 16,
						["lv"] = 25,
						["base"] = {
							["str"] = 73,
							["sta"] = 70,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["total"] = {
							["str"] = 73,
							["sta"] = 70,
							["spi"] = 35,
							["agi"] = 52,
							["int"] = 30
						},
						["melee"] = {
							["mhMin"] = 26.74285888671875,
							["attackSpeed"] = 1.96,
							["apPos"] = 0,
							["expertise"] = 0,
							["mhMax"] = 26.74285888671875,
							["apBase"] = 201,
							["hit"] = 0,
							["apNeg"] = 0,
							["mhSpeed"] = 1.96,
							["crit"] = 0,
							["ap"] = 201
						},
						["spells"] = {
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 6603,
								["range"] = 0,
								["n"] = "Atacar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0,
								["spellID"] = 1259416,
								["range"] = 0,
								["n"] = "Caminar sobre el aire"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0.98,
								["spellID"] = 2480,
								["range"] = 30,
								["n"] = "Disparar arco"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2764,
								["range"] = 30,
								["n"] = "Lanzar"
							},
							{
								["kn"] = true,
								["rank"] = "Racial",
								["castTime"] = 0.49,
								["spellID"] = 1259705,
								["range"] = 0,
								["n"] = "Read Leyline"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259710,
								["range"] = 0,
								["n"] = "Bendición del viento"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 107,
								["range"] = 0,
								["n"] = "Bloqueo"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293712,
								["range"] = 0,
								["n"] = "Competencia con armaduras"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 674,
								["range"] = 0,
								["n"] = "Doble empuñadura"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 81,
								["range"] = 0,
								["n"] = "Esquivar"
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1293657,
								["range"] = 0,
								["n"] = "Idiomas"
							},
							{
								["kn"] = true,
								["rank"] = "Pasivo",
								["castTime"] = 0,
								["spellID"] = 3127,
								["range"] = 0,
								["n"] = "Parar"
							},
							{
								["kn"] = true,
								["rank"] = "Pasiva racial",
								["castTime"] = 0,
								["spellID"] = 1259707,
								["range"] = 0,
								["n"] = "Perspicacia elemental"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Abrumar",
								["range"] = 0,
								["spellID"] = 7384
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud de batalla",
								["range"] = 0,
								["spellID"] = 2457
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 402927,
								["range"] = 0,
								["n"] = "Ataque de la victoria"
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Desgarrar",
								["range"] = 0,
								["spellID"] = 6547
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Embestir",
								["range"] = 25,
								["spellID"] = 100
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Falsa arremetida",
								["range"] = 0,
								["spellID"] = 694
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Golpe heroico",
								["range"] = 0,
								["spellID"] = 285
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Represalias",
								["range"] = 0,
								["spellID"] = 20230
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Seccionar",
								["range"] = 0,
								["spellID"] = 1715
							},
							{
								["rank"] = "Rango 2",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Trueno",
								["range"] = 0,
								["spellID"] = 8198
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 1310185,
								["range"] = 0,
								["n"] = "Maestría táctica"
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 1.5,
								["n"] = "Embate",
								["range"] = 0,
								["spellID"] = 1240193
							},
							{
								["rank"] = "Rango 3",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito de guerra",
								["range"] = 0,
								["spellID"] = 6192
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Grito desmoralizador",
								["range"] = 0,
								["spellID"] = 1160
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Mellar",
								["range"] = 0,
								["spellID"] = 845
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Actitud defensiva",
								["range"] = 0,
								["spellID"] = 71
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Azote de escudo",
								["range"] = 0,
								["spellID"] = 72
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 10,
								["castTime"] = 0,
								["n"] = "Bloquear con escudo",
								["range"] = 0,
								["spellID"] = 2565
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 20,
								["castTime"] = 0,
								["n"] = "Desactivar",
								["range"] = 0,
								["spellID"] = 676
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 15,
								["castTime"] = 0,
								["n"] = "Hender armadura",
								["range"] = 0,
								["spellID"] = 7386
							},
							{
								["rank"] = "",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 0,
								["castTime"] = 0,
								["n"] = "Irritar",
								["range"] = 0,
								["spellID"] = 355
							},
							{
								["rank"] = "Rango 1",
								["powerType"] = "RAGE",
								["kn"] = true,
								["cost"] = 5,
								["castTime"] = 0,
								["n"] = "Revancha",
								["range"] = 0,
								["spellID"] = 6572
							},
							{
								["kn"] = true,
								["rank"] = "",
								["castTime"] = 0,
								["spellID"] = 2687,
								["range"] = 0,
								["n"] = "Sanguinario"
							}
						},
						["regen"] = {
							["manaBase"] = 0,
							["manaCasting"] = 0
						},
						["resist"] = {
							["holy"] = 0,
							["arcane"] = 0,
							["shadow"] = 0,
							["fire"] = 0,
							["frost"] = 0,
							["nature"] = 4
						},
						["powerMax"] = 100,
						["armorEffective"] = 1274,
						["defense"] = {
							["parry"] = 6.92,
							["block"] = 4.92,
							["blockValue"] = 9,
							["dodge"] = 5.7
						},
						["spell"] = {
							["power"] = 0,
							["haste"] = 0,
							["crit"] = 0,
							["hit"] = 0
						},
						["healthMax"] = 794,
						["powerType"] = "RAGE",
						["stance"] = "Actitud defensiva"
					}
				},
				["seq"] = 1245,
				["msgType"] = 12
			},
			{
				["at"] = 1795175546,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						900
					},
					["oseq"] = 1246,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175545,
						["duration"] = 17,
						["targets"] = {},
						["died"] = true,
						["inInstance"] = false,
						["startedAt"] = 1795175528,
						["death"] = {
							["killer"] = "",
							["lastHits"] = {
								{
									["spellName"] = "Descarga de las Sombras",
									["source"] = "",
									["amount"] = 418
								},
								{
									["spellName"] = "Atacar",
									["source"] = "",
									["amount"] = 94
								}
							}
						},
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 512,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 512
								}
							}
						},
						["id"] = 900
					}
				},
				["seq"] = 1246,
				["msgType"] = 7
			},
			{
				["at"] = 1795175688,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						901
					},
					["oseq"] = 1247,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175687,
						["duration"] = 32,
						["targets"] = {
							{
								["pn"] = 787,
								["classification"] = "normal",
								["lv"] = 27,
								["n"] = "Sanador esquelético"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795175655,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 262,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 262
								}
							}
						},
						["id"] = 901
					}
				},
				["seq"] = 1247,
				["msgType"] = 7
			},
			{
				["at"] = 1795175717,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"npcs",
						311
					},
					["oseq"] = 1248,
					["v"] = {
						["y"] = 0.342,
						["x"] = 0.079,
						["n"] = "Sven Yorgen",
						["r"] = 5,
						["m"] = 1431,
						["fs"] = 1795172711,
						["pn"] = 311,
						["qg"] = true
					}
				},
				["seq"] = 1248,
				["msgType"] = 9
			},
			{
				["at"] = 1795175717,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"quests",
						323
					},
					["oseq"] = 1249,
					["v"] = {
						["fs"] = 1795172712,
						["dt"] = "Morbent Vil es un Necromántico y un aliado de los Jinetes Oscuros. Te enfrentaría al poder de Morbent Vil, pero no te enviaré a luchar contra ese demonio sin conocer tu fuerza.\n\nYa me has probado tu valentía pero, si de verdad quieres enfrentarte al Necromántico, tendrás que probar primero que puedes derrotar a sus esbirros.",
						["ot"] = "Mata a 15 asaltantes esqueléticos, 3 sanadores esqueléticos y 3 guardas esqueléticos y vuelve a donde Sven.",
						["lv"] = 28,
						["gy"] = 0.341,
						["gx"] = 0.079,
						["tm"] = 1431,
						["gna"] = "Sven Yorgen",
						["tn"] = 311,
						["ti"] = "Prueba de valor",
						["tx"] = 0.079,
						["ty"] = 0.342,
						["rw"] = {
							["it"] = {},
							["mo"] = 1800,
							["xp"] = 2300
						},
						["qd"] = 323,
						["gm"] = 1431,
						["gn"] = 311,
						["tna"] = "Sven Yorgen"
					}
				},
				["seq"] = 1249,
				["msgType"] = 9
			},
			{
				["at"] = 1795175717,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"quests",
						269
					},
					["oseq"] = 1250,
					["v"] = {
						["gna"] = "Sven Yorgen",
						["fs"] = 1795175717,
						["ti"] = "En busca de sabiduría",
						["dt"] = "Ve a la ciudad de Ventormenta y habla con el obispo Farthing. Búscalo en la Catedral de la Luz. Dale el diario del Gato y dile que deseas enfrentarte a Morbent Vil.\n\nAunque Ventormenta ha abandonado a Villa Oscura, Farthing es un alma buena y sabia. Puede que te ayude a salvarnos.\n\nTu misión es peligrosa, pero date prisa, Guerrera. Las sombras del Bosque del Ocaso son más cada vez más oscuras.",
						["rw"] = {
							["it"] = {},
							["mo"] = 0,
							["xp"] = 600
						},
						["ot"] = "Entrega el diario completo de Alterio al obispo Lontananza de Ventormenta.",
						["gn"] = 311,
						["qd"] = 269,
						["gm"] = 1431,
						["gy"] = 0.342,
						["gx"] = 0.079
					}
				},
				["seq"] = 1250,
				["msgType"] = 9
			},
			{
				["at"] = 1795175733,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"quests",
						269
					},
					["oseq"] = 1251,
					["v"] = {
						["gna"] = "Sven Yorgen",
						["fs"] = 1795175717,
						["ti"] = "En busca de sabiduría",
						["dt"] = "Ve a la ciudad de Ventormenta y habla con el obispo Farthing. Búscalo en la Catedral de la Luz. Dale el diario del Gato y dile que deseas enfrentarte a Morbent Vil.\n\nAunque Ventormenta ha abandonado a Villa Oscura, Farthing es un alma buena y sabia. Puede que te ayude a salvarnos.\n\nTu misión es peligrosa, pero date prisa, Guerrera. Las sombras del Bosque del Ocaso son más cada vez más oscuras.",
						["rw"] = {
							["it"] = {},
							["mo"] = 0,
							["xp"] = 600
						},
						["lv"] = 29,
						["ot"] = "Entrega el diario completo de Alterio al obispo Lontananza de Ventormenta.",
						["gn"] = 311,
						["qd"] = 269,
						["gm"] = 1431,
						["gy"] = 0.342,
						["gx"] = 0.079
					}
				},
				["seq"] = 1251,
				["msgType"] = 9
			},
			{
				["at"] = 1795175733,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"questChains",
						269
					},
					["oseq"] = 1252,
					["v"] = {
						["pre"] = {
							323
						},
						["next"] = 270
					}
				},
				["seq"] = 1252,
				["msgType"] = 16
			},
			{
				["at"] = 1795175792,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						902
					},
					["oseq"] = 1253,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175791,
						["duration"] = 10.8,
						["targets"] = {
							{
								["pn"] = 217,
								["classification"] = "normal",
								["lv"] = 20,
								["n"] = "Telarácnida venenosa"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795175781,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 109,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 109
								}
							}
						},
						["id"] = 902
					}
				},
				["seq"] = 1253,
				["msgType"] = 7
			},
			{
				["at"] = 1795175953,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						903
					},
					["oseq"] = 1254,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175952,
						["duration"] = 13.4,
						["targets"] = {
							{
								["pn"] = 923,
								["classification"] = "normal",
								["lv"] = 24,
								["n"] = "Nicroyermo joven"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795175938,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 73,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 73
								}
							}
						},
						["id"] = 903
					}
				},
				["seq"] = 1254,
				["msgType"] = 7
			},
			{
				["at"] = 1795175986,
				["shares"] = 0,
				["payload"] = {
					["p"] = {
						"combats",
						904
					},
					["oseq"] = 1255,
					["v"] = {
						["damageDone"] = {
							["total"] = 0,
							["dps"] = 0,
							["spells"] = {}
						},
						["zone"] = "Bosque del Ocaso",
						["endedAt"] = 1795175985,
						["duration"] = 10.9,
						["targets"] = {
							{
								["pn"] = 923,
								["classification"] = "normal",
								["lv"] = 24,
								["n"] = "Nicroyermo joven"
							}
						},
						["died"] = false,
						["inInstance"] = false,
						["startedAt"] = 1795175974,
						["m"] = 1431,
						["healingDone"] = {
							["total"] = 0,
							["spells"] = {}
						},
						["damageTaken"] = {
							["total"] = 49,
							["sources"] = {
								{
									["spellID"] = 0,
									["n"] = "",
									["amount"] = 49
								}
							}
						},
						["id"] = 904
					}
				},
				["seq"] = 1255,
				["msgType"] = 7
			}
		},
		["seq"] = 1255,
		["win"] = 14
	}
}
