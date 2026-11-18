package bridge

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMsgQuestionIsRegistered(t *testing.T) {
	if MsgQuestion != 15 {
		t.Fatalf("MsgQuestion = %d, the addon sends 15", MsgQuestion)
	}
	if MsgNames[MsgQuestion] != "question" {
		t.Fatalf("name = %q", MsgNames[MsgQuestion])
	}
}

func TestParseQuestion(t *testing.T) {
	body := []byte(`{"id":12,"text":"  ¿qué hago ahora?  ","at":1700000000,"mapID":85,"target":null}`)
	q, err := ParseQuestion(body)
	if err != nil {
		t.Fatal(err)
	}
	if q.ID != 12 || q.Text != "¿qué hago ahora?" || q.MapID != 85 || q.At != 1700000000 || q.Target != nil {
		t.Fatalf("parsed %+v", q)
	}
}

func TestParseQuestionTarget(t *testing.T) {
	q, err := ParseQuestion([]byte(`{"id":1,"text":"hola","target":"Thrall"}`))
	if err != nil {
		t.Fatal(err)
	}
	if q.Target == nil || *q.Target != "Thrall" {
		t.Fatalf("target = %v", q.Target)
	}
}

func TestParseQuestionRejects(t *testing.T) {
	cases := map[string]string{
		"not json":      `{"id":`,
		"no id":         `{"text":"hola"}`,
		"id zero":       `{"id":0,"text":"hola"}`,
		"empty text":    `{"id":3,"text":"   "}`,
		"missing text":  `{"id":3}`,
		"text is array": `{"id":3,"text":["a"]}`,
	}
	for name, body := range cases {
		if _, err := ParseQuestion([]byte(body)); !errors.Is(err, ErrBadQuestion) {
			t.Fatalf("%s: err = %v, want ErrBadQuestion", name, err)
		}
	}
}

func TestParseQuestionCutsLongText(t *testing.T) {
	long := strings.Repeat("ñ", MaxQuestionRunes+40)
	q, err := ParseQuestion([]byte(`{"id":4,"text":"` + long + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(q.Text); n != MaxQuestionRunes {
		t.Fatalf("text has %d runes, want %d", n, MaxQuestionRunes)
	}
}

// A question message reaches the subscribers once, when its last chunk arrives, and the body is
// stored under the question type.
func TestQuestionMessageReachesSubscribers(t *testing.T) {
	c := NewChannel()
	var got []uint8
	var bodies []string
	c.OnMessage(func(typ uint8, body []byte, _ time.Time) {
		got = append(got, typ)
		bodies = append(bodies, string(body))
	})

	now := time.Unix(1700000000, 0)
	body := `{"id":7,"text":"¿dónde entrego?","mapID":85}`
	// Two chunks of the same message, out of order.
	first := body[:20]
	second := body[20:]
	c.Feed(FrameV3{MsgType: MsgQuestion, MsgID: 9, ChunkIndex: 1, ChunkCount: 2, Data: []byte(second)}, now)
	if len(got) != 0 {
		t.Fatal("a partial message must not be delivered")
	}
	c.Feed(FrameV3{MsgType: MsgQuestion, MsgID: 9, ChunkIndex: 0, ChunkCount: 2, Data: []byte(first)}, now)
	if len(got) != 1 || got[0] != MsgQuestion {
		t.Fatalf("delivered %v, want one question", got)
	}
	if bodies[0] != body {
		t.Fatalf("body = %q", bodies[0])
	}

	q, err := ParseQuestion([]byte(bodies[0]))
	if err != nil || q.ID != 7 {
		t.Fatalf("parse of the delivered body: %+v, %v", q, err)
	}
	if it, ok := c.Data.Get(MsgQuestion); !ok || it.Name != "question" {
		t.Fatalf("question not stored in LiveData: %+v %v", it, ok)
	}
}

func TestOnMessageIgnoresNilAndKeepsOrder(t *testing.T) {
	c := NewChannel()
	c.OnMessage(nil)
	var order []int
	c.OnMessage(func(uint8, []byte, time.Time) { order = append(order, 1) })
	c.OnMessage(func(uint8, []byte, time.Time) { order = append(order, 2) })
	c.Feed(FrameV3{MsgType: MsgQuestion, MsgID: 1, ChunkIndex: 0, ChunkCount: 1, Data: []byte(`{"id":1,"text":"x"}`)}, time.Now())
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("subscriber order = %v", order)
	}
}
