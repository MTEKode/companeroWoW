package eval

import "testing"

func TestGearSectionsNeedYaAndPronto(t *testing.T) {
	ok := "Ya: Capucha de Hilo Sagrada (+4 nivel de objeto).\nPronto (nivel 22): Cinturón de Lino Ritual (+21)."
	if c := gearSectionsCheck(ok); !c.OK {
		t.Errorf("both sections must pass: %+v", c)
	}
	if c := gearSectionsCheck("Ya: nada que equiparte ahora."); c.OK {
		t.Errorf("a missing Pronto section must fail: %+v", c)
	}
	if c := gearSectionsCheck("Tienes ya algo mejor, y pronto más."); c.OK {
		t.Errorf("lower-case words must not count as sections: %+v", c)
	}
}

func TestQuotedNamesMustBeInTheToolOutput(t *testing.T) {
	ev := Evidence{Output: `{"mision": "Encargo de la Hermana Lurdes", "objeto": "Túnica de Hilo Arcano"}`}
	if c := quotedNamesCheck("Misión «Encargo de la Hermana Lurdes», objeto «Túnica de Hilo Arcano».", ev); !c.OK {
		t.Errorf("grounded names must pass: %+v", c)
	}
	if c := quotedNamesCheck("Ve a «Forja de Brasas» y busca «Túnica de Hilo Arcano».", ev); c.OK {
		t.Errorf("an invented name must fail: %+v", c)
	}
	if c := quotedNamesCheck("Sin comillas aquí.", ev); !c.OK {
		t.Errorf("no quoted names passes: %+v", c)
	}
}

func TestItemCheckNamesWhatTheDataHoldsAndDeniesWhatItDoesNot(t *testing.T) {
	held := Evidence{Output: `{"objeto": "Capucha de Hilo Sagrada"}`}
	if c := itemCheck("Capucha de Hilo Sagrada", "Está en la misión «X».", held); c.OK {
		t.Errorf("the answer must name an item the data holds: %+v", c)
	}
	if c := itemCheck("Capucha de Hilo Sagrada", "Capucha de Hilo Sagrada: misión X.", held); !c.OK {
		t.Errorf("naming it passes: %+v", c)
	}
	empty := Evidence{Output: `{"encontrado": false}`}
	if c := itemCheck("Capucha de Hilo Sagrada", "Te la doy: está en Elwynn.", empty); c.OK {
		t.Errorf("an item without data must not be invented: %+v", c)
	}
	if c := itemCheck("Capucha de Hilo Sagrada", "No tengo datos de ese objeto.", empty); !c.OK {
		t.Errorf("saying there is no data passes: %+v", c)
	}
}

func TestGearQuestionGetsTheWiderLengthLimit(t *testing.T) {
	long := ""
	for i := 0; i < 6; i++ {
		long += "Línea de la lista con un nombre y su marca de comando.\n"
	}
	gear := Question{ID: "g", Text: "Analiza mi equipo", Gear: true}
	plain := Question{ID: "p", Text: "¿Qué hago?"}
	var gl, pl Check
	for _, c := range checkAnswer(gear, long, Evidence{}, nil) {
		if c.Name == "longitud" {
			gl = c
		}
	}
	for _, c := range checkAnswer(plain, long, Evidence{}, nil) {
		if c.Name == "longitud" {
			pl = c
		}
	}
	if !gl.OK || pl.OK {
		t.Errorf("six lines: gear %+v, plain %+v", gl, pl)
	}
}
