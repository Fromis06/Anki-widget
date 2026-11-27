package anki

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestExtractSoundTags(t *testing.T) {
	raw := `<style>.card { color: black; }</style>
		<div class="word">日本語[にほんご] を 勉強[べんきょう]する</div>
		[sound:nihongo.mp3]
		<hr id=answer>
		[sound:nihongo.mp3][sound:example.ogg]`

	cleaned, audio := ExtractSoundTags(raw)

	wantAudio := []string{"nihongo.mp3", "example.ogg"}
	if !reflect.DeepEqual(audio, wantAudio) {
		t.Fatalf("audio mismatch: got %v, want %v", audio, wantAudio)
	}

	wantSubstrings := []string{
		"<ruby>日本語<rt>にほんご</rt></ruby>",
		"<ruby>勉強<rt>べんきょう</rt></ruby>",
	}
	for _, sub := range wantSubstrings {
		if !contains(cleaned, sub) {
			t.Errorf("cleaned HTML missing %q; got %q", sub, cleaned)
		}
	}

	forbidden := []string{"[sound:", "<style>", "<hr", "<div"}
	for _, bad := range forbidden {
		if contains(cleaned, bad) {
			t.Errorf("cleaned HTML should not contain %q; got %q", bad, cleaned)
		}
	}
}

func TestClientFlowWithMockAnkiConnect(t *testing.T) {
	var answeredEase int
	var answeredCardID int64
	var syncCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ankiconnectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		writeResult := func(res interface{}) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": res,
				"error":  nil,
			})
		}

		switch req.Action {
		case "deckNames":
			writeResult([]string{"Japanese::Core2k", "EmptyDeck"})
		case "findCards":
			params := req.Params.(map[string]interface{})
			q := params["query"].(string)
			switch {
			case contains(q, "EmptyDeck"):
				writeResult([]int64{})
			case contains(q, "is:learn"):
				writeResult([]int64{})
			case contains(q, "is:due"):
				writeResult([]int64{1001, 1002, 1003, 1004})
			case contains(q, "is:new"):
				writeResult([]int64{2001, 2002})
			default:
				writeResult([]int64{1001})
			}
		case "getDeckStats":
			// Scheduler-capped counts (lower than raw findCards lengths 4/2)
			writeResult(map[string]DeckStats{
				"123": {
					DeckID:      123,
					Name:        "Japanese::Core2k",
					NewCount:    1,
					LearnCount:  1,
					ReviewCount: 1,
				},
			})
		case "findModelsByName":
			writeResult([]ankiModelDefinition{
				{
					Name: "Soumatome",
					Type: 0,
					Tmpls: []struct {
						Name string `json:"name"`
						Ord  int    `json:"ord"`
						Qfmt string `json:"qfmt"`
						Afmt string `json:"afmt"`
					}{
						{
							Name: "Card 1",
							Ord:  0,
							Qfmt: `{{Front}}`,
							Afmt: `{{FrontSide}}<hr id="answer">{{Back}}`,
						},
						{
							Name: "Card 2",
							Ord:  1,
							Qfmt: `{{Meaning}}`,
							Afmt: `{{FrontSide}}<hr id="answer">{{Front}}{{Audio}}`,
						},
					},
				},
			})
		case "cardsInfo":
			writeResult([]CardInfo{
				{
					CardID:    1001,
					DeckName:  "Japanese::Core2k",
					ModelName: "Soumatome",
					Ord:       1,
					Fields: map[string]FieldData{
						"Front":   {Value: "猫[ねこ]", Order: 0},
						"Meaning": {Value: "Con mèo (Cat)[sound:neko.mp3]", Order: 1},
						"Audio":   {Value: "[sound:neko_ex.ogg]", Order: 2},
					},
				},
			})
		case "answerCards":
			params := req.Params.(map[string]interface{})
			answers := params["answers"].([]interface{})
			first := answers[0].(map[string]interface{})
			answeredCardID = int64(first["cardId"].(float64))
			answeredEase = int(first["ease"].(float64))
			writeResult([]bool{true})
		case "sync":
			syncCalled = true
			writeResult(nil)
		case "version":
			writeResult(6)
		case "guiCurrentCard":
			writeResult(nil)
		case "retrieveMediaFile":
			writeResult(base64.StdEncoding.EncodeToString([]byte("fake-ogg-bytes")))
		default:
			t.Fatalf("unexpected action: %s", req.Action)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)

	decks, err := client.DeckNames(true)
	if err != nil {
		t.Fatalf("DeckNames failed: %v", err)
	}
	if !reflect.DeepEqual(decks, []string{"Japanese::Core2k"}) {
		t.Fatalf("unexpected filtered decks: %v", decks)
	}

	card, err := client.FetchNextCard("Japanese::Core2k")
	if err != nil {
		t.Fatalf("FetchNextCard failed: %v", err)
	}
	// getDeckStats capped counts: learn(1)+review(1)=2 due, new=1, total=3 (not raw findCards 4+2=6)
	if card.CardID != 1001 || !card.IsDue || card.RemainingDue != 2 || card.RemainingNew != 1 || card.TotalRemaining != 3 {
		t.Fatalf("unexpected ParsedCard metadata: %+v", card)
	}
	if card.FrontHTML != "Con mèo (Cat)" {
		t.Fatalf("unexpected FrontHTML for ord=1: %q", card.FrontHTML)
	}
	if card.BackHTML != "<ruby>猫<rt>ねこ</rt></ruby>" {
		t.Fatalf("unexpected BackHTML for ord=1: %q", card.BackHTML)
	}
	if !reflect.DeepEqual(card.FrontAudio, []string{"neko.mp3"}) {
		t.Fatalf("unexpected FrontAudio: %v", card.FrontAudio)
	}
	if !reflect.DeepEqual(card.BackAudio, []string{"neko_ex.ogg"}) {
		t.Fatalf("unexpected BackAudio: %v", card.BackAudio)
	}

	if err := client.AnswerCard(card.CardID, 3); err != nil {
		t.Fatalf("AnswerCard failed: %v", err)
	}
	if answeredCardID != 1001 || answeredEase != 3 {
		t.Fatalf("unexpected AnswerCard payload: card=%d ease=%d", answeredCardID, answeredEase)
	}

	if err := client.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if !syncCalled {
		t.Fatalf("expected Sync to invoke ankiconnect sync action")
	}

	// Verify MediaHandler serves from disk when present and falls back to retrieveMediaFile.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "neko.mp3"), []byte("local-mp3-data"), 0o644); err != nil {
		t.Fatalf("write temp media: %v", err)
	}
	handler := client.MediaHandler(func() string { return tmpDir })

	reqLocal := httptest.NewRequest(http.MethodGet, "/media/neko.mp3", nil)
	recLocal := httptest.NewRecorder()
	handler.ServeHTTP(recLocal, reqLocal)
	if recLocal.Code != http.StatusOK || recLocal.Body.String() != "local-mp3-data" {
		t.Fatalf("expected local media serve, got code=%d body=%q", recLocal.Code, recLocal.Body.String())
	}

	reqRPC := httptest.NewRequest(http.MethodGet, "/media/neko_ex.ogg", nil)
	recRPC := httptest.NewRecorder()
	handler.ServeHTTP(recRPC, reqRPC)
	if recRPC.Code != http.StatusOK || recRPC.Body.String() != "fake-ogg-bytes" {
		t.Fatalf("expected RPC media fallback, got code=%d body=%q", recRPC.Code, recRPC.Body.String())
	}
}

func TestClientFetchNextCardFallbacks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ankiconnectRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeErr := func(msg string) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": nil, "error": msg})
		}
		writeRes := func(res interface{}) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": res, "error": nil})
		}

		switch req.Action {
		case "findCards":
			params := req.Params.(map[string]interface{})
			if contains(params["query"].(string), "is:due") {
				writeRes([]int64{42})
			} else {
				writeRes([]int64{})
			}
		case "getDeckStats", "findModelsByName":
			writeErr("unsupported action")
		case "cardsInfo":
			writeRes([]CardInfo{
				{
					CardID:    42,
					DeckName:  "Deck",
					ModelName: "Unknown",
					Question:  `<style>.a{}</style><div>FrontQ[sound:q.mp3]</div>`,
					Answer:    `<div>FrontQ</div><hr id="answer" style="margin:4px"><div>BackA</div>`,
					Fields: map[string]FieldData{
						"Front": {Value: "FrontQ[sound:q.mp3]", Order: 0},
						"Back":  {Value: "BackA", Order: 1},
					},
				},
			})
		default:
			writeErr("unexpected")
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	card, err := client.FetchNextCard("Deck")
	if err != nil {
		t.Fatalf("FetchNextCard fallback failed: %v", err)
	}
	if card.FrontHTML != "FrontQ" || card.BackHTML != "BackA" {
		t.Fatalf("unexpected fallback HTML: front=%q back=%q", card.FrontHTML, card.BackHTML)
	}
	if !reflect.DeepEqual(card.FrontAudio, []string{"q.mp3"}) {
		t.Fatalf("unexpected fallback FrontAudio: %v", card.FrontAudio)
	}
}

func TestSQLiteStoreFlowSyncDeductionAndTemplates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "collection.anki2")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}

	crt := time.Now().Add(-10 * 24 * time.Hour).Unix()
	initialLS := time.Now().Add(-1 * time.Hour).UnixMilli()
	stmts := []string{
		`CREATE TABLE col (ver integer NOT NULL, crt integer NOT NULL, mod integer NOT NULL DEFAULT 0, ls integer NOT NULL DEFAULT 0);`,
		`CREATE TABLE config (key text PRIMARY KEY NOT NULL, val blob NOT NULL);`,
		`CREATE TABLE decks (id integer PRIMARY KEY NOT NULL, name text NOT NULL, mtime_secs integer NOT NULL DEFAULT 0, usn integer NOT NULL DEFAULT 0, common blob NOT NULL DEFAULT X'', kind blob NOT NULL DEFAULT X'');`,
		`CREATE TABLE deck_config (id integer PRIMARY KEY NOT NULL, name text NOT NULL, mtime_secs integer NOT NULL DEFAULT 0, usn integer NOT NULL DEFAULT 0, config blob NOT NULL);`,
		`CREATE TABLE notetypes (id integer PRIMARY KEY NOT NULL, name text NOT NULL, mtime_secs integer NOT NULL DEFAULT 0, usn integer NOT NULL DEFAULT 0, config blob NOT NULL DEFAULT X'');`,
		`CREATE TABLE fields (ntid integer NOT NULL, ord integer NOT NULL, name text NOT NULL, PRIMARY KEY (ntid, ord));`,
		`CREATE TABLE templates (ntid integer NOT NULL, ord integer NOT NULL, name text NOT NULL, mtime_secs integer NOT NULL DEFAULT 0, usn integer NOT NULL DEFAULT 0, config blob NOT NULL, PRIMARY KEY (ntid, ord));`,
		`CREATE TABLE notes (id integer PRIMARY KEY NOT NULL, mid integer NOT NULL, flds text NOT NULL, tags text NOT NULL DEFAULT '');`,
		`CREATE TABLE cards (id integer PRIMARY KEY NOT NULL, nid integer NOT NULL, did integer NOT NULL, ord integer NOT NULL, mod integer NOT NULL, usn integer NOT NULL, type integer NOT NULL, queue integer NOT NULL, due integer NOT NULL, ivl integer NOT NULL, factor integer NOT NULL, reps integer NOT NULL, lapses integer NOT NULL, left integer NOT NULL, odue integer NOT NULL, odid integer NOT NULL, flags integer NOT NULL, data text NOT NULL);`,
		`CREATE TABLE revlog (id integer PRIMARY KEY NOT NULL, cid integer NOT NULL, usn integer NOT NULL, ease integer NOT NULL, ivl integer NOT NULL, lastIvl integer NOT NULL, factor integer NOT NULL, time integer NOT NULL, type integer NOT NULL);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("create schema %q: %v", s, err)
		}
	}

	_, err = db.Exec(`INSERT INTO col (ver, crt, mod, ls) VALUES (18, ?, ?, ?)`, crt, initialLS, initialLS)
	if err != nil {
		t.Fatalf("insert col: %v", err)
	}
	_, err = db.Exec(`INSERT INTO config (key, val) VALUES ('collapseTime', '1200')`)
	if err != nil {
		t.Fatalf("insert config: %v", err)
	}

	// Deck config 1: unlimited; Deck config 2: capped at 1 new / 1 review per day
	if _, err := db.Exec(`INSERT INTO deck_config (id, name, config) VALUES (1, 'Default', ?), (2, 'Capped1', ?)`,
		buildTestDeckConfig(9999, 9999),
		buildTestDeckConfig(1, 1),
	); err != nil {
		t.Fatalf("insert deck_config: %v", err)
	}

	// Parent decks use config 1, leaf Vocab uses capped config 2; preserve unknown field 255 in common
	var leafCommonWithOther []byte
	otherBytes := `{"desiredRetention":null}`
	leafCommonWithOther = append(leafCommonWithOther, 0xfa, 0x0f, byte(len(otherBytes)))
	leafCommonWithOther = append(leafCommonWithOther, otherBytes...)

	if _, err := db.Exec(
		`INSERT INTO decks (id, name, common, kind) VALUES
		 (1, 'Default', X'', ?),
		 (9, 'Yapanese', X'', ?),
		 (10, 'Yapanese' || char(31) || 'N2', X'', ?),
		 (11, 'Yapanese' || char(31) || 'N2' || char(31) || 'Vocab', ?, ?)`,
		buildTestDeckKind(1), buildTestDeckKind(1), buildTestDeckKind(1), leafCommonWithOther, buildTestDeckKind(2),
	); err != nil {
		t.Fatalf("insert decks: %v", err)
	}

	ntCfg := buildTestNotetypeConfig(false, `.hidden-reading { visibility: hidden; }`)
	if _, err := db.Exec(`INSERT INTO notetypes (id, name, config) VALUES (50, 'Soumatome', ?)`, ntCfg); err != nil {
		t.Fatalf("insert notetypes: %v", err)
	}
	for ord, name := range []string{"Từ vựng", "Furigana", "Hán tự", "Nghĩa", "Audio"} {
		if _, err := db.Exec(`INSERT INTO fields (ntid, ord, name) VALUES (50, ?, ?)`, ord, name); err != nil {
			t.Fatalf("insert field %s: %v", name, err)
		}
	}

	// ord=0: Từ vựng on Front (Furigana hidden), Back has Nghĩa + Furigana + Audio
	// ord=1: Nghĩa on Front, Back has Audio + Từ vựng + Furigana
	tmpl0 := buildTestTemplateConfig(
		`<div>{{Từ vựng}}</div><div class="hidden-reading">{{Furigana}}</div>`,
		`{{FrontSide}}<hr id="answer"><div>{{Nghĩa}}</div><div>{{Furigana}}</div><div>{{Audio}}</div>`,
	)
	tmpl1 := buildTestTemplateConfig(
		`<div>{{Nghĩa}}</div>`,
		`{{FrontSide}}<hr id="answer"><div>{{Audio}}</div><div>{{Từ vựng}}</div><div>{{Furigana}}</div>`,
	)
	if _, err := db.Exec(
		`INSERT INTO templates (ntid, ord, name, config) VALUES (50, 0, 'Thẻ 1', ?), (50, 1, 'Thẻ 2', ?)`,
		tmpl0, tmpl1,
	); err != nil {
		t.Fatalf("insert templates: %v", err)
	}

	flds := "約束\x1fやくそく\x1fƯỚC THÚC\x1fLời hứa\x1f[sound:yakusoku.mp3]"
	if _, err := db.Exec(`INSERT INTO notes (id, mid, flds, tags) VALUES (500, 50, ?, 'N2')`, flds); err != nil {
		t.Fatalf("insert note: %v", err)
	}

	// Two due review cards (9001 ord=1, 9002 ord=0) + two new cards (9003, 9004) in capped deck 11 (limit 1 rev / 1 new)
	if _, err := db.Exec(
		`INSERT INTO cards (id, nid, did, ord, mod, usn, type, queue, due, ivl, factor, reps, lapses, left, odue, odid, flags, data) VALUES
		 (9001, 500, 11, 1, 1000, 5, 2, 2, 1, 5, 2500, 3, 0, 0, 0, 0, 0, '{"pos":1,"s":4.2}'),
		 (9002, 500, 11, 0, 1000, 5, 2, 2, 2, 5, 2500, 3, 0, 0, 0, 0, 0, '{}'),
		 (9003, 500, 11, 0, 1000, 5, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, '{}'),
		 (9004, 500, 11, 1, 1000, 5, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, '{}')`,
	); err != nil {
		t.Fatalf("insert cards: %v", err)
	}
	_ = db.Close()

	decks, err := SQLiteDeckNames(dbPath, true)
	if err != nil {
		t.Fatalf("SQLiteDeckNames failed: %v", err)
	}
	wantDecks := []string{"Yapanese", "Yapanese::N2", "Yapanese::N2::Vocab"}
	if !reflect.DeepEqual(decks, wantDecks) {
		t.Fatalf("SQLiteDeckNames mismatch: got %v, want %v", decks, wantDecks)
	}

	// 1. Subdeck matching + deck_config quota capping (1 rev + 1 new, NOT 2 + 2) + ord=1 template rendering
	card, err := SQLiteFetchNextCard(dbPath, "Yapanese::N2")
	if err != nil {
		t.Fatalf("SQLiteFetchNextCard failed: %v", err)
	}
	if card.CardID != 9001 || card.RemainingDue != 1 || card.RemainingNew != 1 || card.TotalRemaining != 2 {
		t.Fatalf("unexpected capped card metadata: %+v", card)
	}
	if card.FrontHTML != "Lời hứa" {
		t.Fatalf("expected ord=1 FrontHTML 'Lời hứa', got %q", card.FrontHTML)
	}
	if !contains(card.BackHTML, "約束") || !contains(card.BackHTML, "やくそく") || contains(card.BackHTML, "Lời hứa") {
		t.Fatalf("unexpected ord=1 BackHTML: %q", card.BackHTML)
	}
	if len(card.FrontAudio) != 0 || !reflect.DeepEqual(card.BackAudio, []string{"yakusoku.mp3"}) {
		t.Fatalf("expected back-only audio for ord=1: front=%v back=%v", card.FrontAudio, card.BackAudio)
	}

	// 2. Answer 9001 with Good (ease=3) and elapsedMs=4321
	if err := SQLiteAnswerCardWithElapsed(dbPath, 9001, 3, 4321); err != nil {
		t.Fatalf("SQLiteAnswerCardWithElapsed failed: %v", err)
	}

	// Verify sync flags (col.mod > col.ls), card data keys preserved, revlog time=4321, and decks.common updated across ancestors
	verifyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer verifyDB.Close()

	var colMod, colLS int64
	if err := verifyDB.QueryRow(`SELECT mod, ls FROM col LIMIT 1`).Scan(&colMod, &colLS); err != nil {
		t.Fatalf("query col mod/ls: %v", err)
	}
	if colMod <= colLS {
		t.Fatalf("expected col.mod (%d) > col.ls (%d) for sync detection", colMod, colLS)
	}

	var usn, ivl, reps int
	var due int64
	var dataJSON string
	if err := verifyDB.QueryRow(`SELECT usn, ivl, reps, due, data FROM cards WHERE id = 9001`).Scan(&usn, &ivl, &reps, &due, &dataJSON); err != nil {
		t.Fatalf("verify card 9001: %v", err)
	}
	if usn != -1 || ivl <= 5 || reps != 4 || due <= 10 {
		t.Fatalf("unexpected updated card 9001: usn=%d ivl=%d reps=%d due=%d", usn, ivl, reps, due)
	}
	if !contains(dataJSON, `"pos":1`) || !contains(dataJSON, `"s":4.2`) || !contains(dataJSON, `"lrt":`) {
		t.Fatalf("expected cards.data to preserve pos & s and set lrt; got %s", dataJSON)
	}

	var revCount, revUSN, revEase, revTime int
	if err := verifyDB.QueryRow(`SELECT COUNT(*), MIN(usn), MIN(ease), MIN(time) FROM revlog WHERE cid = 9001`).Scan(&revCount, &revUSN, &revEase, &revTime); err != nil {
		t.Fatalf("verify revlog 9001: %v", err)
	}
	if revCount != 1 || revUSN != -1 || revEase != 3 || revTime != 4321 {
		t.Fatalf("unexpected revlog entry: count=%d usn=%d ease=%d time=%d", revCount, revUSN, revEase, revTime)
	}

	// Verify decks.common & usn=-1 updated on target deck 11 AND ancestor decks 10 and 9
	for _, did := range []int64{9, 10, 11} {
		var dUSN int
		var dCommon []byte
		if err := verifyDB.QueryRow(`SELECT usn, common FROM decks WHERE id = ?`, did).Scan(&dUSN, &dCommon); err != nil {
			t.Fatalf("query deck %d: %v", did, err)
		}
		dc := decodeDeckCommon(dCommon)
		if dUSN != -1 || dc.LastDayStudied != 10 || dc.ReviewStudied != 1 || dc.MillisecondsStudied != 4321 {
			t.Fatalf("unexpected deck %d sync/common state: usn=%d common=%+v", did, dUSN, dc)
		}
		if did == 11 && len(dc.UnknownFields) != 1 {
			t.Fatalf("expected unknown field 255 preserved on deck 11, got %+v", dc.UnknownFields)
		}
	}

	// 3. Review quota for deck 11 is now 0 (1 limit - 1 studied), so next fetch MUST skip 9002 and serve new card 9003!
	nextCard, err := SQLiteFetchNextCard(dbPath, "Yapanese::N2")
	if err != nil {
		t.Fatalf("SQLiteFetchNextCard after review quota failed: %v", err)
	}
	if nextCard.CardID != 9003 || nextCard.RemainingDue != 0 || nextCard.RemainingNew != 1 || nextCard.TotalRemaining != 1 {
		t.Fatalf("expected new card 9003 after review quota reached, got %+v", nextCard)
	}
	// ord=0 template must hide .hidden-reading on Front!
	if nextCard.FrontHTML != "約束" || contains(nextCard.FrontHTML, "やくそく") {
		t.Fatalf("expected ord=0 FrontHTML '約束' without hidden reading, got %q", nextCard.FrontHTML)
	}

	// 4. Answer brand-new card 9003 with Good (ease=3) -> must enter learning (type=1, queue=1, left=1, revlog.factor=0)
	// and increment new_studied=1 so new quota is now 0 (9004 is NOT served!).
	if err := SQLiteAnswerCardWithElapsed(dbPath, 9003, 3, 2500); err != nil {
		t.Fatalf("SQLiteAnswerCardWithElapsed new card failed: %v", err)
	}

	var cType, cQueue, cLeft int
	var cDue int64
	if err := verifyDB.QueryRow(`SELECT type, queue, due, left FROM cards WHERE id = 9003`).Scan(&cType, &cQueue, &cDue, &cLeft); err != nil {
		t.Fatalf("verify new card 9003: %v", err)
	}
	if cType != 1 || cQueue != 1 || cLeft != 1 || cDue <= time.Now().Unix()+500 {
		t.Fatalf("expected new card 9003 to enter learning step (type=1, queue=1, left=1, due~+600s), got type=%d queue=%d left=%d due=%d", cType, cQueue, cLeft, cDue)
	}
	var revFactor int
	if err := verifyDB.QueryRow(`SELECT factor FROM revlog WHERE cid = 9003`).Scan(&revFactor); err != nil {
		t.Fatalf("verify revlog 9003: %v", err)
	}
	if revFactor != 0 {
		t.Fatalf("expected revlog.factor=0 for learning review, got %d", revFactor)
	}

	// Verify learn-ahead fallback picks 9003 (due within collapseTime=1200s) and NOT quota-exhausted 9004
	learnAheadCard, err := SQLiteFetchNextCard(dbPath, "Yapanese::N2")
	if err != nil {
		t.Fatalf("SQLiteFetchNextCard learn-ahead failed: %v", err)
	}
	if learnAheadCard.CardID != 9003 || learnAheadCard.RemainingDue != 1 || learnAheadCard.RemainingNew != 0 {
		t.Fatalf("expected learn-ahead card 9003 with RemainingNew=0, got %+v", learnAheadCard)
	}
}

func TestSQLiteBlocksWriteWhenAnkiRunning(t *testing.T) {
	orig := isAnkiRunningFunc
	isAnkiRunningFunc = func() bool { return true }
	defer func() { isAnkiRunningFunc = orig }()

	err := SQLiteAnswerCard(filepath.Join(t.TempDir(), "collection.anki2"), 123, 3)
	if err == nil {
		t.Fatalf("expected SQLiteAnswerCard to refuse write while Anki Desktop is running")
	}
}

func TestSQLiteFailedCardDoesNotImmediatelyRePopupWhenOtherCardsReady(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "collection.anki2")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	crt := time.Now().Add(-5 * 24 * time.Hour).Unix()
	for _, s := range []string{
		`CREATE TABLE col (ver integer NOT NULL, crt integer NOT NULL, mod integer NOT NULL DEFAULT 0, ls integer NOT NULL DEFAULT 0);`,
		`CREATE TABLE decks (id integer PRIMARY KEY NOT NULL, name text NOT NULL);`,
		`CREATE TABLE notetypes (id integer PRIMARY KEY NOT NULL, name text NOT NULL);`,
		`CREATE TABLE fields (ntid integer NOT NULL, ord integer NOT NULL, name text NOT NULL, PRIMARY KEY (ntid, ord));`,
		`CREATE TABLE notes (id integer PRIMARY KEY NOT NULL, mid integer NOT NULL, flds text NOT NULL);`,
		`CREATE TABLE cards (id integer PRIMARY KEY NOT NULL, nid integer NOT NULL, did integer NOT NULL, ord integer NOT NULL, mod integer NOT NULL, usn integer NOT NULL, type integer NOT NULL, queue integer NOT NULL, due integer NOT NULL, ivl integer NOT NULL, factor integer NOT NULL, reps integer NOT NULL, lapses integer NOT NULL, left integer NOT NULL, odue integer NOT NULL, odid integer NOT NULL, flags integer NOT NULL, data text NOT NULL);`,
		`CREATE TABLE revlog (id integer PRIMARY KEY NOT NULL, cid integer NOT NULL, usn integer NOT NULL, ease integer NOT NULL, ivl integer NOT NULL, lastIvl integer NOT NULL, factor integer NOT NULL, time integer NOT NULL, type integer NOT NULL);`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	_, _ = db.Exec(`INSERT INTO col (ver, crt) VALUES (18, ?)`, crt)
	_, _ = db.Exec(`INSERT INTO decks (id, name) VALUES (1, 'Default')`)
	_, _ = db.Exec(`INSERT INTO notetypes (id, name) VALUES (1, 'Basic')`)
	_, _ = db.Exec(`INSERT INTO fields (ntid, ord, name) VALUES (1, 0, 'Front'), (1, 1, 'Back')`)
	_, _ = db.Exec(`INSERT INTO notes (id, mid, flds) VALUES (10, 1, 'Q1' || char(31) || 'A1'), (11, 1, 'Q2' || char(31) || 'A2')`)
	_, _ = db.Exec(`INSERT INTO cards (id, nid, did, ord, mod, usn, type, queue, due, ivl, factor, reps, lapses, left, odue, odid, flags, data) VALUES
		(101, 10, 1, 0, 0, 0, 2, 2, 1, 5, 2500, 1, 0, 0, 0, 0, 0, '{}'),
		(102, 11, 1, 0, 0, 0, 2, 2, 2, 5, 2500, 1, 0, 0, 0, 0, 0, '{}')`)
	_ = db.Close()

	// Answer card 101 with Again (ease=1) -> moves to queue=1, due=nowSec+600
	if err := SQLiteAnswerCard(dbPath, 101, 1); err != nil {
		t.Fatalf("answer 101 again: %v", err)
	}

	// Next fetch must serve ready due card 102, NOT immediately re-popup future queue=1 card 101!
	next, err := SQLiteFetchNextCard(dbPath, "Default")
	if err != nil {
		t.Fatalf("fetch next: %v", err)
	}
	if next.CardID != 102 {
		t.Fatalf("expected ready card 102 instead of failed card 101 in cooldown, got %d", next.CardID)
	}
}

func TestClientPingStatusDistinguishesTimeoutAndRefused(t *testing.T) {
	// 1. Connection refused on closed port -> available=false, timedOut=false
	closedClient := NewClient("http://127.0.0.1:1")
	avail, timedOut, err := closedClient.PingStatus()
	if avail || timedOut || err == nil {
		t.Fatalf("expected closed socket to report available=false, timedOut=false; got avail=%v timedOut=%v err=%v", avail, timedOut, err)
	}

	// 2. Slow server (>250ms first ping, <1500ms retry) -> retry succeeds!
	srvSlow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(350 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": 6, "error": nil})
	}))
	defer srvSlow.Close()

	slowClient := NewClient(srvSlow.URL)
	avail, timedOut, err = slowClient.PingStatus()
	if !avail || timedOut || err != nil {
		t.Fatalf("expected retry ping to succeed on 350ms response; got avail=%v timedOut=%v err=%v", avail, timedOut, err)
	}
}

func TestSQLiteCollectionDayFastAndCachesCRT(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "collection.anki2")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	crt := time.Now().Add(-3 * 24 * time.Hour).Unix()
	if _, err := db.Exec(`CREATE TABLE col (ver integer NOT NULL, crt integer NOT NULL); INSERT INTO col (ver, crt) VALUES (18, ?);`, crt); err != nil {
		t.Fatalf("seed col: %v", err)
	}
	_ = db.Close()

	orig := isAnkiRunningFunc
	isAnkiRunningFunc = func() bool { return false }
	defer func() { isAnkiRunningFunc = orig }()

	day1, err := SQLiteCollectionDay(dbPath)
	if err != nil || day1 == "" {
		t.Fatalf("SQLiteCollectionDay error: %v", err)
	}

	// Even when Anki Desktop starts running later, cached CRT computes day instantaneously without touching SQLite.
	isAnkiRunningFunc = func() bool { return true }
	start := time.Now()
	day2, err := SQLiteCollectionDay(dbPath)
	if err != nil || day2 != day1 {
		t.Fatalf("expected cached day %q when Anki running, got %q err=%v", day1, day2, err)
	}
	if time.Since(start) > 20*time.Millisecond {
		t.Fatalf("expected cached SQLiteCollectionDay < 20ms, took %v", time.Since(start))
	}
}

func TestClientStudyModes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ankiconnectRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeRes := func(res interface{}) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": res, "error": nil})
		}
		switch req.Action {
		case "findCards":
			params := req.Params.(map[string]interface{})
			q := params["query"].(string)
			switch {
			case contains(q, "is:learn"):
				writeRes([]int64{})
			case contains(q, "is:due"):
				writeRes([]int64{1001, 1002})
			case contains(q, "is:new"):
				writeRes([]int64{2001, 2002})
			default:
				writeRes([]int64{})
			}
		case "getDeckStats":
			writeRes(map[string]DeckStats{
				"1": {DeckID: 1, Name: "Deck", NewCount: 2, LearnCount: 0, ReviewCount: 2},
			})
		case "guiCurrentCard":
			// Simulate Anki Desktop currently showing due card 1001
			writeRes(CardInfo{CardID: 1001, DeckName: "Deck"})
		case "cardsInfo":
			params := req.Params.(map[string]interface{})
			ids := params["cards"].([]interface{})
			cid := int64(ids[0].(float64))
			writeRes([]CardInfo{{
				CardID:   cid,
				DeckName: "Deck",
				Fields: map[string]FieldData{
					"Front": {Value: "Q", Order: 0},
					"Back":  {Value: "A", Order: 1},
				},
			}})
		default:
			writeRes(nil)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)

	// 1. Due Only: serves due card 1001, RemainingDue=2, RemainingNew=0, TotalRemaining=2
	dueCard, err := client.FetchNextCardWithOptions("Deck", nil, nil, "due")
	if err != nil {
		t.Fatalf("due mode error: %v", err)
	}
	if dueCard.CardID != 1001 || !dueCard.IsDue || dueCard.RemainingDue != 2 || dueCard.RemainingNew != 0 || dueCard.TotalRemaining != 2 {
		t.Fatalf("unexpected due-only card: %+v", dueCard)
	}

	// 2. New Only: skips due guiCurrentCard 1001 and serves new card 2001 with RemainingDue=0
	newCard, err := client.FetchNextCardWithOptions("Deck", nil, nil, "new")
	if err != nil {
		t.Fatalf("new mode error: %v", err)
	}
	if newCard.CardID != 2001 || newCard.IsDue || newCard.RemainingDue != 0 || newCard.RemainingNew != 2 || newCard.TotalRemaining != 2 {
		t.Fatalf("unexpected new-only card: %+v", newCard)
	}

	// 3. Both: includes both counts (TotalRemaining=4)
	bothCard, err := client.FetchNextCardWithOptions("Deck", nil, nil, "both")
	if err != nil {
		t.Fatalf("both mode error: %v", err)
	}
	if bothCard.CardID != 1001 || !bothCard.IsDue || bothCard.RemainingDue != 2 || bothCard.RemainingNew != 2 || bothCard.TotalRemaining != 4 {
		t.Fatalf("unexpected both-mode card: %+v", bothCard)
	}
}

func TestSQLiteStudyModes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "collection.anki2")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	crt := time.Now().Add(-5 * 24 * time.Hour).Unix()
	for _, s := range []string{
		`CREATE TABLE col (ver integer NOT NULL, crt integer NOT NULL, mod integer NOT NULL DEFAULT 0, ls integer NOT NULL DEFAULT 0);`,
		`CREATE TABLE decks (id integer PRIMARY KEY NOT NULL, name text NOT NULL);`,
		`CREATE TABLE notetypes (id integer PRIMARY KEY NOT NULL, name text NOT NULL);`,
		`CREATE TABLE fields (ntid integer NOT NULL, ord integer NOT NULL, name text NOT NULL, PRIMARY KEY (ntid, ord));`,
		`CREATE TABLE notes (id integer PRIMARY KEY NOT NULL, mid integer NOT NULL, flds text NOT NULL);`,
		`CREATE TABLE cards (id integer PRIMARY KEY NOT NULL, nid integer NOT NULL, did integer NOT NULL, ord integer NOT NULL, mod integer NOT NULL, usn integer NOT NULL, type integer NOT NULL, queue integer NOT NULL, due integer NOT NULL, ivl integer NOT NULL, factor integer NOT NULL, reps integer NOT NULL, lapses integer NOT NULL, left integer NOT NULL, odue integer NOT NULL, odid integer NOT NULL, flags integer NOT NULL, data text NOT NULL);`,
		`CREATE TABLE revlog (id integer PRIMARY KEY NOT NULL, cid integer NOT NULL, usn integer NOT NULL, ease integer NOT NULL, ivl integer NOT NULL, lastIvl integer NOT NULL, factor integer NOT NULL, time integer NOT NULL, type integer NOT NULL);`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	_, _ = db.Exec(`INSERT INTO col (ver, crt) VALUES (18, ?)`, crt)
	_, _ = db.Exec(`INSERT INTO decks (id, name) VALUES (1, 'Default')`)
	_, _ = db.Exec(`INSERT INTO notetypes (id, name) VALUES (1, 'Basic')`)
	_, _ = db.Exec(`INSERT INTO fields (ntid, ord, name) VALUES (1, 0, 'Front'), (1, 1, 'Back')`)
	_, _ = db.Exec(`INSERT INTO notes (id, mid, flds) VALUES (10, 1, 'DueQ' || char(31) || 'DueA'), (11, 1, 'NewQ' || char(31) || 'NewA')`)
	_, _ = db.Exec(`INSERT INTO cards (id, nid, did, ord, mod, usn, type, queue, due, ivl, factor, reps, lapses, left, odue, odid, flags, data) VALUES
		(301, 10, 1, 0, 0, 0, 2, 2, 1, 5, 2500, 1, 0, 0, 0, 0, 0, '{}'),
		(302, 11, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, '{}')`)
	_ = db.Close()

	// 1. New Only skips due card 301 and serves new card 302
	newCard, err := SQLiteFetchNextCardWithOptions(dbPath, "Default", nil, nil, "new")
	if err != nil {
		t.Fatalf("sqlite new mode: %v", err)
	}
	if newCard.CardID != 302 || newCard.IsDue || newCard.RemainingDue != 0 || newCard.RemainingNew != 1 || newCard.TotalRemaining != 1 {
		t.Fatalf("unexpected sqlite new-only card: %+v", newCard)
	}

	// 2. Due Only serves due card 301 and reports RemainingNew=0
	dueCard, err := SQLiteFetchNextCardWithOptions(dbPath, "Default", nil, nil, "due")
	if err != nil {
		t.Fatalf("sqlite due mode: %v", err)
	}
	if dueCard.CardID != 301 || !dueCard.IsDue || dueCard.RemainingDue != 1 || dueCard.RemainingNew != 0 || dueCard.TotalRemaining != 1 {
		t.Fatalf("unexpected sqlite due-only card: %+v", dueCard)
	}

	// 3. Both reports 1 due + 1 new = 2 total
	bothCard, err := SQLiteFetchNextCardWithOptions(dbPath, "Default", nil, nil, "both")
	if err != nil {
		t.Fatalf("sqlite both mode: %v", err)
	}
	if bothCard.CardID != 301 || bothCard.RemainingDue != 1 || bothCard.RemainingNew != 1 || bothCard.TotalRemaining != 2 {
		t.Fatalf("unexpected sqlite both-mode card: %+v", bothCard)
	}

	// 4. After answering due card 301, Due Only returns TotalRemaining=0 (does NOT spill into new card 302!)
	if err := SQLiteAnswerCard(dbPath, 301, 3); err != nil {
		t.Fatalf("answer 301: %v", err)
	}
	doneDue, err := SQLiteFetchNextCardWithOptions(dbPath, "Default", nil, nil, "due")
	if err != nil {
		t.Fatalf("sqlite due mode after finish: %v", err)
	}
	if doneDue.CardID != 0 || doneDue.TotalRemaining != 0 || doneDue.RemainingDue != 0 || doneDue.RemainingNew != 0 {
		t.Fatalf("expected empty card in due-only mode after finishing due cards, got %+v", doneDue)
	}
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && indexString(s, substr) >= 0)
}

func indexString(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
