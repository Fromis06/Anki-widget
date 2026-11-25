package anki

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func buildTestTemplateConfig(qfmt, afmt string) []byte {
	var buf []byte
	buf = binary.AppendUvarint(buf, (1<<3)|2)
	buf = binary.AppendUvarint(buf, uint64(len(qfmt)))
	buf = append(buf, qfmt...)
	buf = binary.AppendUvarint(buf, (2<<3)|2)
	buf = binary.AppendUvarint(buf, uint64(len(afmt)))
	buf = append(buf, afmt...)
	return buf
}

func buildTestNotetypeConfig(isCloze bool, css string) []byte {
	var buf []byte
	if isCloze {
		buf = appendProtoVarintField(buf, 1, 1)
	}
	if css != "" {
		buf = binary.AppendUvarint(buf, (3<<3)|2)
		buf = binary.AppendUvarint(buf, uint64(len(css)))
		buf = append(buf, css...)
	}
	return buf
}

func buildTestDeckKind(configID int64) []byte {
	var normal []byte
	normal = appendProtoVarintField(normal, 1, uint64(configID))
	var wrapper []byte
	wrapper = binary.AppendUvarint(wrapper, (1<<3)|2)
	wrapper = binary.AppendUvarint(wrapper, uint64(len(normal)))
	wrapper = append(wrapper, normal...)
	return wrapper
}

func buildTestDeckConfig(newPerDay, reviewsPerDay uint32) []byte {
	var buf []byte
	buf = appendProtoVarintField(buf, 9, uint64(newPerDay))
	buf = appendProtoVarintField(buf, 10, uint64(reviewsPerDay))
	return buf
}

func TestProtobufDecodersAndRoundTrip(t *testing.T) {
	// Unknown field 255 ("other") must be preserved verbatim through decode -> encode.
	otherPayload := `{"desiredRetention":null}`
	var raw []byte
	raw = appendProtoVarintField(raw, 1, 1)   // study_collapsed = true
	raw = appendProtoVarintField(raw, 3, 498) // last_day_studied
	raw = appendProtoVarintField(raw, 4, 2)   // new_studied
	raw = binary.AppendUvarint(raw, (255<<3)|2)
	raw = binary.AppendUvarint(raw, uint64(len(otherPayload)))
	raw = append(raw, otherPayload...)

	dc := decodeDeckCommon(raw)
	if !dc.StudyCollapsed || dc.LastDayStudied != 498 || dc.NewStudied != 2 {
		t.Fatalf("unexpected decoded Deck.Common: %+v", dc)
	}
	dc.ReviewStudied = 5
	reencoded := encodeDeckCommon(dc)
	dc2 := decodeDeckCommon(reencoded)
	if dc2.ReviewStudied != 5 || dc2.NewStudied != 2 || len(dc2.UnknownFields) != 1 {
		t.Fatalf("round-trip failed to preserve fields/unknowns: %+v", dc2)
	}

	kind := decodeDeckKind(buildTestDeckKind(1772277118307))
	if kind.ConfigID != 1772277118307 || kind.IsFiltered {
		t.Fatalf("unexpected decoded DeckKind: %+v", kind)
	}

	newLim, revLim := decodeDeckConfigLimits(buildTestDeckConfig(60, 600))
	if newLim != 60 || revLim != 600 {
		t.Fatalf("unexpected deck config limits: new=%d rev=%d", newLim, revLim)
	}

	qfmt, afmt := decodeTemplateConfig(buildTestTemplateConfig("{{Front}}", "{{Back}}"))
	if qfmt != "{{Front}}" || afmt != "{{Back}}" {
		t.Fatalf("unexpected template config: q=%q a=%q", qfmt, afmt)
	}

	isCloze, css := decodeNotetypeConfig(buildTestNotetypeConfig(true, ".card { color: red; }"))
	if !isCloze || css != ".card { color: red; }" {
		t.Fatalf("unexpected notetype config: isCloze=%v css=%q", isCloze, css)
	}
}

func TestRenderRealCollectionNotetypesAndTemplates(t *testing.T) {
	cases := []struct {
		name           string
		tmpl           CardTemplate
		fields         map[string]FieldData
		tags           string
		deck           string
		model          string
		wantFront      []string
		forbidFront    []string
		wantBack       []string
		forbidBack     []string
		wantFrontAudio []string
		wantBackAudio  []string
	}{
		{
			name: "tu_vung_jp_ord0_front_word_and_audio",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "Thẻ 2",
				Qfmt: `<main class="n2-shell"><header class="n2-top"><span class="n2-badge">JLPT N2</span><span>TỪ VỰNG · {{stt}}</span></header><div class="n2-field n2-word">{{tu vung}}</div><div class="n2-audio">{{audio}}</div><p class="n2-hint">Nhớ cách đọc và nghĩa của từ</p></main><script>alert(1)</script>`,
				Afmt: `<main class="n2-shell"><div class="n2-field n2-word">{{tu vung}}</div>{{#cach doc}}<div class="n2-field n2-reading">{{cach doc}}</div>{{/cach doc}}{{#am han}}<div class="n2-field n2-han">{{am han}}</div>{{/am han}}{{#nghia}}<section class="n2-section"><div class="n2-label">NGHĨA</div><div class="n2-field n2-meaning">{{nghia}}</div></section>{{/nghia}}</main>`,
			},
			fields: map[string]FieldData{
				"stt":      {Value: "12"},
				"tu vung":  {Value: "約束"},
				"cach doc": {Value: "やくそく"},
				"am han":   {Value: "ƯỚC THÚC"},
				"nghia":    {Value: "Lời hứa"},
				"audio":    {Value: "[sound:yakusoku.mp3]"},
			},
			wantFront:      []string{"約束", "12"},
			forbidFront:    []string{"やくそく", "Lời hứa", "<script>", "alert(1)", "[sound:"},
			wantBack:       []string{"約束", "やくそく", "ƯỚC THÚC", "Lời hứa"},
			wantFrontAudio: []string{"yakusoku.mp3"},
			wantBackAudio:  nil,
		},
		{
			name: "tu_vung_jp_ord1_front_reading",
			tmpl: CardTemplate{
				Ord:  1,
				Name: "Thẻ 1",
				Qfmt: `<div class="n2-word kana">{{cach doc}}</div><div class="n2-audio">{{audio}}</div>`,
				Afmt: `<div class="n2-word">{{tu vung}}</div>{{#nghia}}<div>{{nghia}}</div>{{/nghia}}`,
			},
			fields: map[string]FieldData{
				"tu vung":  {Value: "約束"},
				"cach doc": {Value: "やくそく"},
				"nghia":    {Value: "Lời hứa"},
				"audio":    {Value: "[sound:yakusoku.mp3]"},
			},
			wantFront:      []string{"やくそく"},
			forbidFront:    []string{"約束", "Lời hứa"},
			wantBack:       []string{"約束", "Lời hứa"},
			wantFrontAudio: []string{"yakusoku.mp3"},
		},
		{
			name: "TongHopNguPhapN3_FrontSide_and_hr_answer_dedup",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "NguphapN3",
				Qfmt: `<span style="font-size: 35px;">{{term}}</span><div style="font-size: 15px;">{{Tags}}</div>`,
				Afmt: `<script src="_anki-tooltips.js"></script>{{FrontSide}}<hr id=answer><!DOCTYPE html><html><head><style>td{color:red}</style></head><body><table><tr><td>{{vietnamese}}</td></tr><tr><td>{{definition}}</td></tr><tr><td>{{example}}</td></tr></table></body></html>`,
			},
			fields: map[string]FieldData{
				"term":       {Value: "〜うちに"},
				"vietnamese": {Value: "Trong lúc..."},
				"definition": {Value: "Diễn tả hành động tranh thủ"},
				"example":    {Value: "<ruby>若<rt>わか</rt></ruby>いうちに"},
			},
			tags:        "N3",
			wantFront:   []string{"〜うちに", "N3"},
			wantBack:    []string{"Trong lúc...", "Diễn tả hành động tranh thủ", "<ruby>若<rt>わか</rt></ruby>いうちに"},
			forbidBack:  []string{"〜うちに", "<style>", "<script>", "<table", "<td", "<hr"},
		},
		{
			name: "passjapanesetest_JLPT_Vocabulary_furigana_and_FrontSide",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "Card 1",
				Qfmt: `<div class="blue-box">{{furigana:kanji_furigana}}</div>`,
				Afmt: `{{FrontSide}}<hr class="divider"><div class="kana">{{kana}}</div><div>(<strong>{{pos}}</strong>) {{english}}</div><div>{{furigana:ex1_ja_furigana}}<br>{{ex1_en}}</div>{{#ex2_ja}}<div>{{furigana:ex2_ja_furigana}}</div>{{/ex2_ja}}`,
			},
			fields: map[string]FieldData{
				"kanji_furigana":  {Value: "高[たか]い"},
				"kana":            {Value: "たかい"},
				"pos":             {Value: "adj-i"},
				"english":         {Value: "high; expensive"},
				"ex1_ja_furigana": {Value: "車[くるま]が 高[たか]い"},
				"ex1_en":          {Value: "Cars are expensive"},
				"ex2_ja":          {Value: ""},
				"ex2_ja_furigana": {Value: "ShouldNotRender"},
			},
			wantFront:   []string{"<ruby>高<rt>たか</rt></ruby>い"},
			forbidFront: []string{"たかい", "expensive", "<div"},
			wantBack:    []string{"たかい", "(adj-i) high; expensive", "<ruby>車<rt>くるま</rt></ruby>が <ruby>高<rt>たか</rt></ruby>い<br/>Cars are expensive"},
			forbidBack:  []string{"<ruby>高<rt>たか</rt></ruby>い<br/>たかい", "ShouldNotRender", "<strong", "<hr"},
		},
		{
			name: "Soumatome_ord0_type_suppressed_and_back_only_audio",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "Thẻ 1",
				Qfmt: `<div class="blue-box"><div>{{Từ vựng}}</div>{{type:Furigana}}</div>`,
				Afmt: `{{FrontSide}}<hr id="answer"><div class="back-container"><div class="answer-checker">{{type:Furigana}}</div><div>{{Nghĩa}}</div><div>{{Furigana}}</div><div>{{Hán tự}}</div><div>{{Audio}}</div></div>`,
			},
			fields: map[string]FieldData{
				"Từ vựng":  {Value: "改善"},
				"Furigana": {Value: "かいぜん"},
				"Hán tự":   {Value: "CẢI THIỆN"},
				"Nghĩa":    {Value: "Cải thiện"},
				"Audio":    {Value: "[sound:kaizen.mp3]"},
			},
			wantFront:      []string{"改善"},
			forbidFront:    []string{"かいぜん", "Cải thiện"},
			wantBack:       []string{"Cải thiện", "かいぜん", "CẢI THIỆN"},
			forbidBack:     []string{"改善", "[sound:"},
			wantFrontAudio: nil,
			wantBackAudio:  []string{"kaizen.mp3"},
		},
		{
			name: "Soumatome_ord1_meaning_front_back_audio",
			tmpl: CardTemplate{
				Ord:  1,
				Name: "Thẻ 2",
				Qfmt: `<div class="blue-box">{{Nghĩa}}</div>`,
				Afmt: `{{FrontSide}}<hr id="answer"><div>{{Audio}}</div><div>{{Từ vựng}}</div><div>{{Furigana}}</div><div>{{Hán tự}}</div>`,
			},
			fields: map[string]FieldData{
				"Từ vựng":  {Value: "改善"},
				"Furigana": {Value: "かいぜん"},
				"Hán tự":   {Value: "CẢI THIỆN"},
				"Nghĩa":    {Value: "Cải thiện"},
				"Audio":    {Value: "[sound:kaizen.mp3]"},
			},
			wantFront:      []string{"Cải thiện"},
			forbidFront:    []string{"改善", "かいぜん"},
			wantBack:       []string{"改善", "かいぜん", "CẢI THIỆN"},
			forbidBack:     []string{"Cải thiện"},
			wantFrontAudio: nil,
			wantBackAudio:  []string{"kaizen.mp3"},
		},
		{
			name: "tuVungN4_hides_anki_reading_hidden_css_and_negated_section",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "Card 1",
				CSS:  `.anki-reading-hidden { font-size: 26px; visibility: hidden; }`,
				Qfmt: `{{#tuVung}}<div class="anki-front">{{tuVung}}</div><div class="anki-reading-hidden">{{cachDoc}}</div>{{/tuVung}}`,
				Afmt: `{{#tuVung}}<div>{{tuVung}}</div><div>{{hanViet}}</div><div>{{cachDoc}}</div>{{/tuVung}}{{^tuVung}}<div>{{cachDoc}}</div>{{/tuVung}}<div><b>{{nghia}}</b></div>`,
			},
			fields: map[string]FieldData{
				"tuVung":  {Value: "傘"},
				"hanViet": {Value: "TẢN"},
				"cachDoc": {Value: "かさ"},
				"nghia":   {Value: "Cái ô"},
			},
			wantFront:   []string{"傘"},
			forbidFront: []string{"かさ", "TẢN", "Cái ô"},
			wantBack:    []string{"傘", "TẢN", "かさ", "Cái ô"},
		},
		{
			name: "N2_Vocabulary_ord0_no_hr_answer_strips_FrontSide",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "Card 1",
				Qfmt: `<div>{{Nghia}}</div>`,
				Afmt: `<div>{{FrontSide}}</div><div>{{TuVung}}</div><div>{{Audio}}</div><div>{{PhienAm}}</div>`,
			},
			fields: map[string]FieldData{
				"Nghia":   {Value: "Hòa giải"},
				"TuVung":  {Value: "仲直り"},
				"PhienAm": {Value: "なかなおり"},
				"Audio":   {Value: "[sound:nakanaori.mp3]"},
			},
			wantFront:      []string{"Hòa giải"},
			wantBack:       []string{"仲直り", "なかなおり"},
			forbidBack:     []string{"Hòa giải"},
			wantFrontAudio: nil,
			wantBackAudio:  []string{"nakanaori.mp3"},
		},
		{
			name: "N2_Vocabulary_ord1_hr_answer_with_inline_style_and_type",
			tmpl: CardTemplate{
				Ord:  1,
				Name: "Card 2",
				Qfmt: `<div>{{TuVung}}</div>{{type:PhienAm}}`,
				Afmt: `<script>x()</script><div>{{FrontSide}}</div><hr id="answer" style="border: none; margin: 4px 0;"><div>{{PhienAm}}</div><div>{{Nghia}}</div>`,
			},
			fields: map[string]FieldData{
				"Nghia":   {Value: "Hòa giải"},
				"TuVung":  {Value: "仲直り"},
				"PhienAm": {Value: "なかなおり"},
			},
			wantFront:   []string{"仲直り"},
			forbidFront: []string{"なかなおり"},
			wantBack:    []string{"なかなおり", "Hòa giải"},
			forbidBack:  []string{"仲直り", "<hr", "<script>"},
		},
		{
			name: "Basic_multi_field_front",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "Card 1",
				Qfmt: `<div>{{Front}}</div><div>{{lala}}</div>`,
				Afmt: `<div>{{Back}}</div><div>{{kaka}}</div>`,
			},
			fields: map[string]FieldData{
				"Front": {Value: "Question A"},
				"lala":  {Value: "Extra Q"},
				"Back":  {Value: "Answer A"},
				"kaka":  {Value: "Extra A"},
			},
			wantFront: []string{"Question A", "Extra Q"},
			wantBack:  []string{"Answer A", "Extra A"},
		},
		{
			name: "ngu_phap_multi_field_front_and_ruby_without_rt",
			tmpl: CardTemplate{
				Ord:  0,
				Name: "NguphapN3",
				Qfmt: `<div lang="ja">{{Word}}<div>{{Sentence}}</div></div>`,
				Afmt: `<div lang="ja">{{Word}}<div><b>[Nghĩa]</b><br>{{Meaning}}</div>{{#Sentence Meaning}}<div>{{Sentence Meaning}}</div>{{/Sentence Meaning}}</div>`,
			},
			fields: map[string]FieldData{
				"Word":             {Value: "〜ばかり"},
				"Sentence":         {Value: "<ruby>勉強</ruby>ばかりしている"},
				"Meaning":          {Value: "Chỉ toàn..."},
				"Sentence Meaning": {Value: "Suốt ngày chỉ học"},
			},
			wantFront: []string{"〜ばかり", "<ruby>勉強</ruby>ばかりしている"},
			wantBack:  []string{"[Nghĩa]<br/>Chỉ toàn...", "Suốt ngày chỉ học"},
		},
		{
			name: "Image_Occlusion_hides_inline_display_none_cloze",
			tmpl: CardTemplate{
				Ord:     0,
				Name:    "Image Occlusion",
				IsCloze: true,
				Qfmt:    `{{#Header}}<div>{{Header}}</div>{{/Header}}<div style="display: none">{{cloze:Occlusion}}</div><canvas id="c"></canvas><script>bad()</script>`,
				Afmt:    `{{#Header}}<div>{{Header}}</div>{{/Header}}<div style="display: none">{{cloze:Occlusion}}</div><div><button id="toggle">Toggle Masks</button></div>{{#Back Extra}}<div>{{Back Extra}}</div>{{/Back Extra}}`,
			},
			fields: map[string]FieldData{
				"Header":     {Value: "Kanji Anatomy"},
				"Occlusion":  {Value: "{{c1::SecretMask}}"},
				"Back Extra": {Value: "Detailed note"},
			},
			wantFront:   []string{"Kanji Anatomy"},
			forbidFront: []string{"SecretMask", "<canvas", "<script"},
			wantBack:    []string{"Kanji Anatomy", "Detailed note"},
			forbidBack:  []string{"SecretMask", "Toggle Masks", "<button"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			front, back, frontAudio, backAudio, _ := RenderCardTemplate(
				tc.tmpl, tc.fields, tc.tags, tc.deck, tc.model,
			)
			for _, w := range tc.wantFront {
				if !strings.Contains(front, w) {
					t.Errorf("FrontHTML missing %q; got %q", w, front)
				}
			}
			for _, bad := range tc.forbidFront {
				if strings.Contains(front, bad) {
					t.Errorf("FrontHTML should not contain %q; got %q", bad, front)
				}
			}
			for _, w := range tc.wantBack {
				if !strings.Contains(back, w) {
					t.Errorf("BackHTML missing %q; got %q", w, back)
				}
			}
			for _, bad := range tc.forbidBack {
				if strings.Contains(back, bad) {
					t.Errorf("BackHTML should not contain %q; got %q", bad, back)
				}
			}
			if !reflect.DeepEqual(frontAudio, tc.wantFrontAudio) {
				t.Errorf("frontAudio mismatch: got %v, want %v", frontAudio, tc.wantFrontAudio)
			}
			if !reflect.DeepEqual(backAudio, tc.wantBackAudio) {
				t.Errorf("backAudio mismatch: got %v, want %v", backAudio, tc.wantBackAudio)
			}
		})
	}
}

func TestSanitizeAnkiHTMLAndFilters(t *testing.T) {
	// XSS / MS Word / general HTML stripping while preserving <br/>, <ruby>/<rt>/<rp>, and safe Anki <img>
	raw := `<!-- comment -->
		<style>.x { color: red; }</style>
		<script>window.pwn=1</script>
		<p class="MsoNormal"><o:p></o:p><font color="#ff0000">注意</font><img src="x" onerror="alert(1)"><img src="javascript:alert(1)"></p>
		<div><img src="dict%20note.png" style="width:999px" onerror="alert(2)"></div>
		<div onclick="bad()">Line 1<br>Line 2</div>
		<ruby class="r" style="color:red">漢<rp>(</rp><rt data-x="1">かん</rt><rp>)</rp></ruby> &lt;safe&gt;`
	got := SanitizeAnkiHTML(raw, "")
	for _, want := range []string{
		`注意<br/><img src="/media/dict%20note.png"/><br/>Line 1<br/>Line 2`,
		`<ruby>漢<rp>(</rp><rt>かん</rt><rp>)</rp></ruby>`,
		`&lt;safe&gt;`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("SanitizeAnkiHTML missing %q; got %q", want, got)
		}
	}
	for _, bad := range []string{"<script", "<style", "onerror", "onclick", "javascript:", "<font", "<p", "<div", `src="x"`, "class=", "style=", "data-x="} {
		if strings.Contains(got, bad) {
			t.Errorf("SanitizeAnkiHTML should strip %q; got %q", bad, got)
		}
	}

	// Filters: kana:, kanji:, text:, cloze:, hint:
	tmpl := CardTemplate{
		Ord:  0,
		Name: "FilterTest",
		Qfmt: `{{kanji:Word}} | {{kana:Word}} | {{text:Styled}} | {{cloze:Cloze}} | {{hint:Hint}}`,
		Afmt: `{{cloze:Cloze}} | {{hint:Hint}}`,
	}
	fields := map[string]FieldData{
		"Word":   {Value: "高[たか]い"},
		"Styled": {Value: `<span style="color:red">Hello</span>`},
		"Cloze":  {Value: `Tokyo is {{c1::Japan::country}} capital {{c2::city}}.`},
		"Hint":   {Value: `East Asia`},
	}
	front, back, _, _, _ := RenderCardTemplate(tmpl, fields, "", "Deck", "Model")
	if !strings.Contains(front, "高い | たかい | Hello | Tokyo is [country] capital city. |") {
		t.Errorf("unexpected filtered Front: %q", front)
	}
	if strings.Contains(front, "East Asia") || strings.Contains(front, "Japan") {
		t.Errorf("Front leaked hint or cloze answer: %q", front)
	}
	if !strings.Contains(back, "Tokyo is Japan capital city. | East Asia") {
		t.Errorf("unexpected filtered Back: %q", back)
	}
}

func TestRenderSelectedFieldsAndExtractFieldNames(t *testing.T) {
	canonical := map[string]string{
		"từ vựng":  "Từ vựng",
		"furigana": "Furigana",
		"hán tự":   "Hán tự",
		"nghĩa":    "Nghĩa",
		"audio":    "Audio",
	}
	frontNames := ExtractTemplateFieldNames(`<div>{{Từ vựng}}</div>{{Audio}}`, canonical)
	if len(frontNames) != 2 || frontNames[0] != "Từ vựng" || frontNames[1] != "Audio" {
		t.Fatalf("unexpected ExtractTemplateFieldNames: %v", frontNames)
	}

	fields := map[string]FieldData{
		"Từ vựng":  {Value: "<b>約束</b>", Order: 0},
		"Furigana": {Value: "やくそく", Order: 1},
		"Hán tự":   {Value: "ƯỚC THÚC", Order: 2},
		"Nghĩa":    {Value: "Lời hứa", Order: 3},
		"Audio":    {Value: "[sound:yakusoku.mp3]", Order: 4},
	}

	front, back, frontAud, backAud, allAud := RenderSelectedFields(
		fields,
		[]string{"Từ vựng", "Audio"},
		[]string{"Furigana", "Nghĩa"},
		"",
	)
	if front != "約束" {
		t.Fatalf("expected only Từ vựng on front, got %q", front)
	}
	if back != "やくそく<br/>Lời hứa" {
		t.Fatalf("expected only Furigana & Nghĩa on back, got %q", back)
	}
	if len(frontAud) != 1 || frontAud[0] != "yakusoku.mp3" || len(backAud) != 0 || len(allAud) != 1 {
		t.Fatalf("unexpected audio split: front=%v back=%v all=%v", frontAud, backAud, allAud)
	}
}
