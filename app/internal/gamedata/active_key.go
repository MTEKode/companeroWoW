package gamedata

// ActiveCharacterKey is the key of the character the app follows (the one most recently
// saved in SavedVariables), or "" when there is none. The game-mode answerer keeps its
// question ids per character with it.
func (g *Service) ActiveCharacterKey() string {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return ""
	}
	return ch.key
}
