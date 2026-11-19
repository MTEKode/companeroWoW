package gamemode

import (
	"strconv"
	"strings"
)

// VoiceToken is one speech voice installed in Windows (a SAPI token): its id, its name and its
// language, the first value of the token's Language attribute, a hex LCID such as "C0A" for
// Spanish (Spain).
type VoiceToken struct {
	ID       string
	Name     string
	Language string
}

// primaryLangSpanish is the primary language id of Spanish (LANG_SPANISH, 0x0A): the low 10 bits
// of an LCID.
const primaryLangSpanish = 0x0A

// primaryLanguage is the first value of a Language attribute ("C0A;409" gives "C0A"), with only
// hex digits kept, so that it can go into the voice XML.
func primaryLanguage(lang string) string {
	if i := strings.IndexByte(lang, ';'); i >= 0 {
		lang = lang[:i]
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(lang) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsSpanish reports whether a Language attribute is Spanish, in any region: the hex LCID's
// primary language is 0x0A.
func IsSpanish(lang string) bool {
	p := primaryLanguage(lang)
	if p == "" {
		return false
	}
	v, err := strconv.ParseUint(p, 16, 32)
	if err != nil {
		return false
	}
	return v&0x3FF == primaryLangSpanish
}

// VoiceChoice is the voice an answer is spoken with.
type VoiceChoice struct {
	Token   VoiceToken
	Spanish bool // the voice speaks Spanish, so the answer asks for it by language
}

// ChooseVoice picks the voice of an answer: the first installed Spanish voice; otherwise the first
// installed voice of any language (the phrase is Spanish, and a voice of another language says it
// with its own accent). ok is false when no voice is installed, and then the tone plays.
func ChooseVoice(voices []VoiceToken) (choice VoiceChoice, ok bool) {
	for _, v := range voices {
		if IsSpanish(v.Language) {
			return VoiceChoice{Token: v, Spanish: true}, true
		}
	}
	if len(voices) == 0 {
		return VoiceChoice{}, false
	}
	return VoiceChoice{Token: voices[0]}, true
}

// Required is the XML voice attribute that selects the chosen voice ("Language=C0A"), or "" when
// the voice is not selected by language and the default voice speaks.
func (c VoiceChoice) Required() string {
	if !c.Spanish {
		return ""
	}
	p := primaryLanguage(c.Token.Language)
	if p == "" {
		return ""
	}
	return "Language=" + p
}

// Describe names the chosen voice for the log: its name and its language.
func (c VoiceChoice) Describe() string {
	name := c.Token.Name
	if name == "" {
		name = c.Token.ID
	}
	lang := primaryLanguage(c.Token.Language)
	if lang == "" {
		return name
	}
	return name + " (" + lang + ")"
}
