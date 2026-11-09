package gamedata

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The rules of the gear answers: which slot an item goes to, whether the class can use it,
// when its source is accessible (now, or soon), and whether it is an upgrade over what is
// equipped. They are pure functions of gearData, so the tests can check them without SavedVariables.

const (
	winNow  = "ahora"
	winSoon = "pronto"
)

// gearGroup is a slot as the player sees it. Rings and trinkets have two inventory slots: the
// weaker of the two is the one an upgrade replaces.
type gearGroup struct {
	Name  string
	Key   string
	Slots []int
}

var gearGroups = []gearGroup{
	{Name: "Cabeza", Key: "HEAD", Slots: []int{1}},
	{Name: "Cuello", Key: "NECK", Slots: []int{2}},
	{Name: "Hombros", Key: "SHOULDER", Slots: []int{3}},
	{Name: "Pecho", Key: "CHEST", Slots: []int{5}},
	{Name: "Cintura", Key: "WAIST", Slots: []int{6}},
	{Name: "Piernas", Key: "LEGS", Slots: []int{7}},
	{Name: "Pies", Key: "FEET", Slots: []int{8}},
	{Name: "Muñecas", Key: "WRIST", Slots: []int{9}},
	{Name: "Manos", Key: "HANDS", Slots: []int{10}},
	{Name: "Anillo", Key: "FINGER", Slots: []int{11, 12}},
	{Name: "Abalorio", Key: "TRINKET", Slots: []int{13, 14}},
	{Name: "Espalda", Key: "BACK", Slots: []int{15}},
	{Name: "Mano derecha", Key: "MAINHAND", Slots: []int{16}},
	{Name: "Mano izquierda", Key: "OFFHAND", Slots: []int{17}},
	{Name: "A distancia", Key: "RANGED", Slots: []int{18}},
}

// equipLocGroup maps the INVTYPE_* location of an item to the key of its slot group.
var equipLocGroup = map[string]string{
	"INVTYPE_HEAD":           "HEAD",
	"INVTYPE_NECK":           "NECK",
	"INVTYPE_SHOULDER":       "SHOULDER",
	"INVTYPE_CHEST":          "CHEST",
	"INVTYPE_ROBE":           "CHEST",
	"INVTYPE_WAIST":          "WAIST",
	"INVTYPE_LEGS":           "LEGS",
	"INVTYPE_FEET":           "FEET",
	"INVTYPE_WRIST":          "WRIST",
	"INVTYPE_HAND":           "HANDS",
	"INVTYPE_FINGER":         "FINGER",
	"INVTYPE_TRINKET":        "TRINKET",
	"INVTYPE_CLOAK":          "BACK",
	"INVTYPE_WEAPON":         "MAINHAND",
	"INVTYPE_WEAPONMAINHAND": "MAINHAND",
	"INVTYPE_2HWEAPON":       "MAINHAND",
	"INVTYPE_WEAPONOFFHAND":  "OFFHAND",
	"INVTYPE_SHIELD":         "OFFHAND",
	"INVTYPE_HOLDABLE":       "OFFHAND",
	"INVTYPE_RANGED":         "RANGED",
	"INVTYPE_RANGEDRIGHT":    "RANGED",
	"INVTYPE_THROWN":         "RANGED",
	"INVTYPE_RELIC":          "RANGED",
}

// groupByKey finds a group by its key.
func groupByKey(key string) (gearGroup, bool) {
	for _, grp := range gearGroups {
		if grp.Key == key {
			return grp, true
		}
	}
	return gearGroup{}, false
}

// groupOf returns the slot group of an item.
func groupOf(it gearItem) (gearGroup, bool) {
	key, ok := equipLocGroup[it.EquipLoc]
	if !ok {
		return gearGroup{}, false
	}
	return groupByKey(key)
}

// groupForSlotArg finds a slot from what the player or the model typed: the Spanish name
// ("cabeza", "mano derecha"), the English key ("HEAD", "mainhand") or the inventory slot id.
func groupForSlotArg(s string) (gearGroup, bool) {
	needle := fold(s)
	if needle == "" {
		return gearGroup{}, false
	}
	for _, grp := range gearGroups {
		if fold(grp.Name) == needle || fold(grp.Key) == needle {
			return grp, true
		}
		if strings.HasPrefix(needle, "anillo") && grp.Key == "FINGER" || strings.HasPrefix(needle, "abalorio") && grp.Key == "TRINKET" {
			return grp, true
		}
	}
	if n, ok := parseSmallInt(needle); ok {
		for _, grp := range gearGroups {
			for _, id := range grp.Slots {
				if id == n {
					return grp, true
				}
			}
		}
	}
	return gearGroup{}, false
}

func parseSmallInt(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, n >= 1 && n <= gearMaxSlot
}

// accentFolds maps the accented letters of Spanish to their plain forms.
var accentFolds = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// fold lower-cases and drops accents so that typed names match the data.
func fold(s string) string {
	return accentFolds.Replace(strings.ToLower(strings.TrimSpace(s)))
}

// usable reports whether the class can equip the item, from the proficiencies the addon
// exported. Without them every item is taken as usable and the answer says so.
func (gd gearData) usable(it gearItem) (bool, string) {
	if !gd.ProfKnown {
		return true, ""
	}
	switch it.Class {
	case "Armor":
		if it.Subclass != "Miscellaneous" && it.Subclass != "Generic" && !containsStr(gd.Armor, it.Subclass) {
			return false, "no puede llevar armadura de tipo " + it.Subclass
		}
	case "Weapon":
		if !containsStr(gd.Weapons, it.Subclass) {
			return false, "no puede usar armas de tipo " + it.Subclass
		}
	}
	if it.EquipLoc == "INVTYPE_WEAPONOFFHAND" && !gd.DualWield {
		return false, "no puede llevar un arma en la mano izquierda"
	}
	return true, ""
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// window tells when a source can be reached: winNow, winSoon, or "" when it cannot be
// (completed, unknown to the quest window, or too high). The rules follow the contract:
// quests come from questWindow, which already limits them to level+3; dungeons are open
// from their minimum level.
func (gd gearData) window(s itemSource) string {
	switch s.Kind {
	case "quest":
		if gd.Completed[s.QuestID] {
			return ""
		}
		q, ok := gd.Quests[s.QuestID]
		if !ok {
			return ""
		}
		if q.Accessible {
			return winNow
		}
		if q.RequiredLevel <= gd.Level+gearSoonLevels || len(q.Missing) > 0 {
			return winSoon
		}
	case "dungeon":
		d, ok := gd.dungeon(s.DungeonKey)
		if !ok {
			return ""
		}
		if gd.Level >= d.Min {
			return winNow
		}
		if d.Min <= gd.Level+gearSoonLevels {
			return winSoon
		}
	}
	return ""
}

// sourceState is the state of a source for answers about one item: where it stands.
func (gd gearData) sourceState(s itemSource) string {
	if s.Kind == "quest" && gd.Completed[s.QuestID] {
		return "ya completada"
	}
	if w := gd.window(s); w != "" {
		return w
	}
	return "fuera de tu nivel"
}

// itemWindow returns the best window of a candidate and its sources in that window. An item
// with any source reachable now is "ahora"; otherwise "pronto" when a source is soon. An
// item with no reachable source returns "".
func (gd gearData) itemWindow(c gearCandidate) (string, []itemSource) {
	for _, want := range []string{winNow, winSoon} {
		var out []itemSource
		for _, s := range c.Sources {
			if gd.window(s) == want {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return want, out
		}
	}
	return "", nil
}

// weakest is the equipped item a group's upgrade replaces: the lowest item level among its
// slots, an empty slot counting as level 0. nil means an empty slot.
func (gd gearData) weakest(grp gearGroup) *gearItem {
	var out *gearItem
	lowest := math.MaxInt
	for _, id := range grp.Slots {
		level := 0
		var it *gearItem
		if e, ok := gd.Equipped[id]; ok {
			cp := e
			it = &cp
			level = e.ItemLevel
		}
		if level < lowest {
			lowest = level
			out = it
		}
	}
	return out
}

// statChange is the difference of one stat between a candidate and the item it replaces.
type statChange struct {
	Key  string
	Diff float64
}

// upgrade is the evaluation of a candidate against its slot.
type upgrade struct {
	Target  *gearItem // what it replaces; nil for an empty slot
	Delta   int       // item level difference
	Changes []statChange
	Notes   []string
}

// bestGain is the largest stat gain of the upgrade.
func (u upgrade) bestGain() float64 {
	best := 0.0
	for _, c := range u.Changes {
		if c.Diff > best {
			best = c.Diff
		}
	}
	return best
}

// evaluate compares a candidate with the slot group. keep is false for a non-upgrade: an item
// level that is not higher, unless some stat gains notably.
func (gd gearData) evaluate(c gearItem, grp gearGroup) (upgrade, bool) {
	target := gd.weakest(grp)
	u := upgrade{Target: target, Delta: c.ItemLevel}
	var curStats map[string]float64
	if target != nil {
		u.Delta = c.ItemLevel - target.ItemLevel
		curStats = target.Stats
	}
	u.Changes = statChanges(c.Stats, curStats)
	if c.EquipLoc == "INVTYPE_2HWEAPON" {
		if off, ok := gd.Equipped[17]; ok {
			u.Notes = append(u.Notes, "Es a dos manos: al equiparla quitas la mano izquierda ("+off.Name+")")
		}
	}
	if grp.Key == "OFFHAND" {
		if mh, ok := gd.Equipped[16]; ok && mh.EquipLoc == "INVTYPE_2HWEAPON" {
			u.Notes = append(u.Notes, "Tu arma a dos manos («"+mh.Name+"») no deja usar mano izquierda: tendrías que quitarla")
		}
	}
	keep := u.Delta >= 1 || (u.bestGain() >= gearNotableStat)
	return u, keep
}

// statChanges lists the stats whose value differs, largest change first (then by key).
func statChanges(cand, cur map[string]float64) []statChange {
	keys := map[string]bool{}
	for k := range cand {
		keys[k] = true
	}
	for k := range cur {
		keys[k] = true
	}
	var out []statChange
	for k := range keys {
		if d := cand[k] - cur[k]; d != 0 {
			out = append(out, statChange{Key: k, Diff: d})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := math.Abs(out[i].Diff), math.Abs(out[j].Diff)
		if ai != aj {
			return ai > aj
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// changesText is "+5 Intelecto, -2 Aguante" in Spanish.
func changesText(list []statChange) string {
	if len(list) == 0 {
		return "sin cambios de stats"
	}
	parts := make([]string, 0, len(list))
	for _, c := range list {
		parts = append(parts, fmt.Sprintf("%s %s", signed(c.Diff), statName(c.Key)))
	}
	return strings.Join(parts, ", ")
}

// statsText is the stats of one item as "Intelecto +5, Aguante +3".
func statsText(stats map[string]float64) string {
	if len(stats) == 0 {
		return ""
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %s", statName(k), signed(stats[k])))
	}
	return strings.Join(parts, ", ")
}

// statNames are the Spanish names of the GetItemStats keys the addon exports.
var statNames = map[string]string{
	"ITEM_MOD_STRENGTH_SHORT":             "Fuerza",
	"ITEM_MOD_AGILITY_SHORT":              "Agilidad",
	"ITEM_MOD_STAMINA_SHORT":              "Aguante",
	"ITEM_MOD_INTELLECT_SHORT":            "Intelecto",
	"ITEM_MOD_SPIRIT_SHORT":               "Espíritu",
	"ITEM_MOD_ATTACK_POWER_SHORT":         "Poder de ataque",
	"ITEM_MOD_RANGED_ATTACK_POWER_SHORT":  "Poder de ataque a distancia",
	"ITEM_MOD_SPELL_POWER_SHORT":          "Poder con hechizos",
	"ITEM_MOD_MANA_REGENERATION_SHORT":    "Regeneración de maná",
	"ITEM_MOD_CRIT_RATING_SHORT":          "Crítico",
	"ITEM_MOD_HASTE_RATING_SHORT":         "Celeridad",
	"ITEM_MOD_HIT_RATING_SHORT":           "Precisión",
	"ITEM_MOD_MASTERY_RATING_SHORT":       "Maestría",
	"ITEM_MOD_DODGE_RATING_SHORT":         "Esquive",
	"ITEM_MOD_PARRY_RATING_SHORT":         "Parada",
	"ITEM_MOD_BLOCK_RATING_SHORT":         "Bloqueo",
	"ITEM_MOD_DEFENSE_SKILL_RATING_SHORT": "Defensa",
	"RESISTANCE0_NAME":                    "Armadura",
	"RESISTANCE1_NAME":                    "Resistencia a lo sagrado",
	"RESISTANCE2_NAME":                    "Resistencia al fuego",
	"RESISTANCE3_NAME":                    "Resistencia a la naturaleza",
	"RESISTANCE4_NAME":                    "Resistencia a la escarcha",
	"RESISTANCE5_NAME":                    "Resistencia a las sombras",
	"RESISTANCE6_NAME":                    "Resistencia a lo arcano",
}

// statName is the Spanish name of a stat key, or the key itself when it is not known.
func statName(key string) string {
	if n, ok := statNames[key]; ok {
		return n
	}
	return key
}

func signed(v float64) string {
	if v > 0 {
		return fmt.Sprintf("+%g", v)
	}
	return fmt.Sprintf("%g", v)
}

// qualityNames are the Spanish names of the item quality colours.
var qualityNames = map[int]string{0: "gris", 1: "blanco", 2: "verde", 3: "azul", 4: "morado", 5: "naranja"}

func qualityName(q int) string {
	if n, ok := qualityNames[q]; ok {
		return n
	}
	return "desconocida"
}

// windowsOf turns the ventana argument into the windows it covers.
func windowsOf(ventana string) (map[string]bool, error) {
	switch fold(ventana) {
	case "", "ambas", "ambos", "todas", "todo":
		return map[string]bool{winNow: true, winSoon: true}, nil
	case "ahora", "ya":
		return map[string]bool{winNow: true}, nil
	case "pronto":
		return map[string]bool{winSoon: true}, nil
	}
	return nil, fmt.Errorf("ventana debe ser ahora, pronto o ambas (recibí %q)", ventana)
}
