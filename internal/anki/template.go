package anki

import (
	"encoding/binary"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	xhtml "golang.org/x/net/html"
)

// CardTemplate holds a card template's Front/Back Mustache templates and model styling.
type CardTemplate struct {
	Ord     int
	Name    string
	Qfmt    string
	Afmt    string
	CSS     string
	IsCloze bool
}

type rawProtoField struct {
	num      uint64
	wireType uint64
	raw      []byte // full encoded tag + payload bytes
}

type deckCommon struct {
	StudyCollapsed      bool
	BrowserCollapsed    bool
	LastDayStudied      uint32
	NewStudied          int32
	ReviewStudied       int32
	LearningStudied     int32
	MillisecondsStudied int32
	UnknownFields       []rawProtoField
}

type dayLimit struct {
	Limit uint32
	Today uint32
}

type deckKindInfo struct {
	ConfigID         int64
	ExtendNew        uint32
	ExtendReview     uint32
	ReviewLimit      *uint32
	NewLimit         *uint32
	ReviewLimitToday *dayLimit
	NewLimitToday    *dayLimit
	IsFiltered       bool
}

func readProtoTag(data []byte, pos int) (fieldNum uint64, wireType uint64, nextPos int, ok bool) {
	if pos >= len(data) {
		return 0, 0, pos, false
	}
	v, n := binary.Uvarint(data[pos:])
	if n <= 0 {
		return 0, 0, pos, false
	}
	return v >> 3, v & 0x7, pos + n, true
}

func skipProtoField(data []byte, wireType uint64, pos int) (int, bool) {
	switch wireType {
	case 0: // varint
		_, n := binary.Uvarint(data[pos:])
		if n <= 0 {
			return pos, false
		}
		return pos + n, true
	case 1: // 64-bit
		if pos+8 > len(data) {
			return pos, false
		}
		return pos + 8, true
	case 2: // length-delimited
		l, n := binary.Uvarint(data[pos:])
		if n <= 0 || pos+n > len(data) || l > uint64(len(data)-(pos+n)) {
			return pos, false
		}
		return pos + n + int(l), true
	case 5: // 32-bit
		if pos+4 > len(data) {
			return pos, false
		}
		return pos + 4, true
	default:
		return pos, false
	}
}

func readProtoVarint(data []byte, pos int) (uint64, int, bool) {
	v, n := binary.Uvarint(data[pos:])
	if n <= 0 {
		return 0, pos, false
	}
	return v, pos + n, true
}

func readProtoBytes(data []byte, pos int) ([]byte, int, bool) {
	l, n := binary.Uvarint(data[pos:])
	if n <= 0 {
		return nil, pos, false
	}
	start := pos + n
	end := start + int(l)
	if end < start || end > len(data) {
		return nil, pos, false
	}
	return data[start:end], end, true
}

func decodeDeckCommon(data []byte) deckCommon {
	var c deckCommon
	pos := 0
	for pos < len(data) {
		start := pos
		fieldNum, wireType, nextPos, ok := readProtoTag(data, pos)
		if !ok {
			break
		}
		pos = nextPos
		handled := false
		switch fieldNum {
		case 1:
			if wireType == 0 {
				if v, np, vok := readProtoVarint(data, pos); vok {
					c.StudyCollapsed = v != 0
					pos = np
					handled = true
				}
			}
		case 2:
			if wireType == 0 {
				if v, np, vok := readProtoVarint(data, pos); vok {
					c.BrowserCollapsed = v != 0
					pos = np
					handled = true
				}
			}
		case 3:
			if wireType == 0 {
				if v, np, vok := readProtoVarint(data, pos); vok {
					c.LastDayStudied = uint32(v)
					pos = np
					handled = true
				}
			}
		case 4:
			if wireType == 0 {
				if v, np, vok := readProtoVarint(data, pos); vok {
					c.NewStudied = int32(v)
					pos = np
					handled = true
				}
			}
		case 5:
			if wireType == 0 {
				if v, np, vok := readProtoVarint(data, pos); vok {
					c.ReviewStudied = int32(v)
					pos = np
					handled = true
				}
			}
		case 6:
			if wireType == 0 {
				if v, np, vok := readProtoVarint(data, pos); vok {
					c.LearningStudied = int32(v)
					pos = np
					handled = true
				}
			}
		case 7:
			if wireType == 0 {
				if v, np, vok := readProtoVarint(data, pos); vok {
					c.MillisecondsStudied = int32(v)
					pos = np
					handled = true
				}
			}
		}
		if !handled {
			endPos, sok := skipProtoField(data, wireType, pos)
			if !sok {
				break
			}
			raw := make([]byte, endPos-start)
			copy(raw, data[start:endPos])
			c.UnknownFields = append(c.UnknownFields, rawProtoField{
				num:      fieldNum,
				wireType: wireType,
				raw:      raw,
			})
			pos = endPos
		}
	}
	return c
}

func appendProtoVarintField(buf []byte, fieldNum uint64, val uint64) []byte {
	buf = binary.AppendUvarint(buf, (fieldNum<<3)|0)
	buf = binary.AppendUvarint(buf, val)
	return buf
}

func encodeDeckCommon(c deckCommon) []byte {
	var buf []byte
	if c.StudyCollapsed {
		buf = appendProtoVarintField(buf, 1, 1)
	}
	if c.BrowserCollapsed {
		buf = appendProtoVarintField(buf, 2, 1)
	}
	if c.LastDayStudied != 0 {
		buf = appendProtoVarintField(buf, 3, uint64(c.LastDayStudied))
	}
	if c.NewStudied != 0 {
		buf = appendProtoVarintField(buf, 4, uint64(int64(c.NewStudied)))
	}
	if c.ReviewStudied != 0 {
		buf = appendProtoVarintField(buf, 5, uint64(int64(c.ReviewStudied)))
	}
	if c.LearningStudied != 0 {
		buf = appendProtoVarintField(buf, 6, uint64(int64(c.LearningStudied)))
	}
	if c.MillisecondsStudied != 0 {
		buf = appendProtoVarintField(buf, 7, uint64(int64(c.MillisecondsStudied)))
	}
	for _, uf := range c.UnknownFields {
		buf = append(buf, uf.raw...)
	}
	return buf
}

func decodeDayLimit(data []byte) dayLimit {
	var dl dayLimit
	pos := 0
	for pos < len(data) {
		fieldNum, wireType, nextPos, ok := readProtoTag(data, pos)
		if !ok {
			break
		}
		pos = nextPos
		if wireType == 0 && (fieldNum == 1 || fieldNum == 2) {
			v, np, vok := readProtoVarint(data, pos)
			if !vok {
				break
			}
			pos = np
			if fieldNum == 1 {
				dl.Limit = uint32(v)
			} else {
				dl.Today = uint32(v)
			}
			continue
		}
		endPos, sok := skipProtoField(data, wireType, pos)
		if !sok {
			break
		}
		pos = endPos
	}
	return dl
}

func decodeDeckKind(data []byte) deckKindInfo {
	info := deckKindInfo{ConfigID: 1}
	pos := 0
	for pos < len(data) {
		fieldNum, wireType, nextPos, ok := readProtoTag(data, pos)
		if !ok {
			break
		}
		pos = nextPos
		if fieldNum == 2 && wireType == 2 {
			info.IsFiltered = true
			endPos, sok := skipProtoField(data, wireType, pos)
			if !sok {
				break
			}
			pos = endPos
			continue
		}
		if fieldNum == 1 && wireType == 2 {
			normalBytes, np, bok := readProtoBytes(data, pos)
			if !bok {
				break
			}
			pos = np
			npos := 0
			for npos < len(normalBytes) {
				nf, nw, nNext, nok := readProtoTag(normalBytes, npos)
				if !nok {
					break
				}
				npos = nNext
				switch {
				case nf == 1 && nw == 0:
					v, vp, vok := readProtoVarint(normalBytes, npos)
					if !vok {
						npos = len(normalBytes)
						break
					}
					info.ConfigID = int64(v)
					npos = vp
				case nf == 2 && nw == 0:
					v, vp, vok := readProtoVarint(normalBytes, npos)
					if !vok {
						npos = len(normalBytes)
						break
					}
					info.ExtendNew = uint32(v)
					npos = vp
				case nf == 3 && nw == 0:
					v, vp, vok := readProtoVarint(normalBytes, npos)
					if !vok {
						npos = len(normalBytes)
						break
					}
					info.ExtendReview = uint32(v)
					npos = vp
				case nf == 6 && nw == 0:
					v, vp, vok := readProtoVarint(normalBytes, npos)
					if !vok {
						npos = len(normalBytes)
						break
					}
					val := uint32(v)
					info.ReviewLimit = &val
					npos = vp
				case nf == 7 && nw == 0:
					v, vp, vok := readProtoVarint(normalBytes, npos)
					if !vok {
						npos = len(normalBytes)
						break
					}
					val := uint32(v)
					info.NewLimit = &val
					npos = vp
				case nf == 8 && nw == 2:
					b, bp, bok2 := readProtoBytes(normalBytes, npos)
					if !bok2 {
						npos = len(normalBytes)
						break
					}
					dl := decodeDayLimit(b)
					info.ReviewLimitToday = &dl
					npos = bp
				case nf == 9 && nw == 2:
					b, bp, bok2 := readProtoBytes(normalBytes, npos)
					if !bok2 {
						npos = len(normalBytes)
						break
					}
					dl := decodeDayLimit(b)
					info.NewLimitToday = &dl
					npos = bp
				default:
					ep, sok := skipProtoField(normalBytes, nw, npos)
					if !sok {
						npos = len(normalBytes)
						break
					}
					npos = ep
				}
			}
			continue
		}
		endPos, sok := skipProtoField(data, wireType, pos)
		if !sok {
			break
		}
		pos = endPos
	}
	return info
}

func decodeDeckConfigLimits(data []byte) (newPerDay uint32, reviewsPerDay uint32) {
	pos := 0
	for pos < len(data) {
		fieldNum, wireType, nextPos, ok := readProtoTag(data, pos)
		if !ok {
			break
		}
		pos = nextPos
		if wireType == 0 && (fieldNum == 9 || fieldNum == 10) {
			v, np, vok := readProtoVarint(data, pos)
			if !vok {
				break
			}
			pos = np
			if fieldNum == 9 {
				newPerDay = uint32(v)
			} else {
				reviewsPerDay = uint32(v)
			}
			continue
		}
		endPos, sok := skipProtoField(data, wireType, pos)
		if !sok {
			break
		}
		pos = endPos
	}
	return newPerDay, reviewsPerDay
}

func decodeTemplateConfig(data []byte) (qfmt, afmt string) {
	pos := 0
	for pos < len(data) {
		fieldNum, wireType, nextPos, ok := readProtoTag(data, pos)
		if !ok {
			break
		}
		pos = nextPos
		if wireType == 2 && (fieldNum == 1 || fieldNum == 2) {
			b, np, bok := readProtoBytes(data, pos)
			if !bok {
				break
			}
			pos = np
			if fieldNum == 1 {
				qfmt = string(b)
			} else {
				afmt = string(b)
			}
			continue
		}
		endPos, sok := skipProtoField(data, wireType, pos)
		if !sok {
			break
		}
		pos = endPos
	}
	return qfmt, afmt
}

func decodeNotetypeConfig(data []byte) (isCloze bool, css string) {
	pos := 0
	for pos < len(data) {
		fieldNum, wireType, nextPos, ok := readProtoTag(data, pos)
		if !ok {
			break
		}
		pos = nextPos
		switch {
		case fieldNum == 1 && wireType == 0:
			v, np, vok := readProtoVarint(data, pos)
			if !vok {
				return isCloze, css
			}
			isCloze = v == 1
			pos = np
		case fieldNum == 3 && wireType == 2:
			b, np, bok := readProtoBytes(data, pos)
			if !bok {
				return isCloze, css
			}
			css = string(b)
			pos = np
		default:
			endPos, sok := skipProtoField(data, wireType, pos)
			if !sok {
				return isCloze, css
			}
			pos = endPos
		}
	}
	return isCloze, css
}

var (
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	hrAnswerTagRe = regexp.MustCompile(`(?is)<hr\b[^>]*\s+id\s*=\s*(?:"answer"[^>]*>|'answer'[^>]*>|answer(?:\s[^>]*>|/[^>]*>|>))`)
	frontSideRe   = regexp.MustCompile(`(?i)\{\{\s*FrontSide\s*\}\}`)
	clozeTagRe    = regexp.MustCompile(`(?s)\{\{c(\d+)::([^}]*?)(?:::([^}]*?))?\}\}`)
	cssCommentRe  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	hiddenDeclRe  = regexp.MustCompile(`(?i)(?:^|;)\s*(?:display\s*:\s*none|visibility\s*:\s*hidden)\s*(?:!\s*important)?\s*(?:;|$)`)
	multiBrRe     = regexp.MustCompile(`(?:<br/>){3,}`)
	spaceBeforeBr = regexp.MustCompile(`[ \t]+<br/>`)
	spaceAfterBr  = regexp.MustCompile(`<br/>[ \t]+`)
	horizSpaceRe  = regexp.MustCompile(`[ \t]{2,}`)
	stripTagRe    = regexp.MustCompile(`<[^>]+>`)
)

// RenderCardTemplate evaluates an Anki card's Front/Back Mustache templates against note fields,
// extracts [sound:...] references scoped separately to Front and Back, strips FrontSide / <hr id="answer">
// duplication on the Back, suppresses CSS-hidden elements, and sanitizes HTML to <ruby>/<rt>/<rp>/<br/>.
func RenderCardTemplate(
	tmpl CardTemplate,
	fields map[string]FieldData,
	tags string,
	deckName string,
	modelName string,
) (frontHTML, backHTML string, frontAudio, backAudio, allAudio []string) {
	fieldValues := make(map[string]string, len(fields))
	fieldValuesLower := make(map[string]string, len(fields))
	for k, v := range fields {
		fieldValues[k] = v.Value
		fieldValuesLower[strings.ToLower(strings.TrimSpace(k))] = v.Value
	}

	subdeck := deckName
	if idx := strings.LastIndex(deckName, "::"); idx >= 0 {
		subdeck = deckName[idx+2:]
	}
	special := map[string]string{
		"Tags":    strings.TrimSpace(tags),
		"Deck":    deckName,
		"Subdeck": subdeck,
		"Card":    tmpl.Name,
		"Type":    modelName,
	}

	rawFront := evaluateMustacheTemplate(tmpl.Qfmt, fieldValues, fieldValuesLower, special, tmpl.Ord, false)
	frontHTML, frontAudio = extractAndSanitizeSide(rawFront, tmpl.CSS)

	rawBack := evaluateMustacheTemplate(tmpl.Afmt, fieldValues, fieldValuesLower, special, tmpl.Ord, true)
	rawBack = extractBackTemplateBody(rawBack)
	backHTML, backAudio = extractAndSanitizeSide(rawBack, tmpl.CSS)

	allAudio = mergeUniqueStrings(frontAudio, backAudio)
	return frontHTML, backHTML, frontAudio, backAudio, allAudio
}

func extractBackTemplateBody(afmt string) string {
	cleaned := htmlCommentRe.ReplaceAllString(afmt, "")
	if loc := hrAnswerTagRe.FindStringIndex(cleaned); loc != nil {
		cleaned = cleaned[loc[1]:]
	}
	cleaned = frontSideRe.ReplaceAllString(cleaned, "")
	return cleaned
}

func extractAndSanitizeSide(rawSide string, css string) (string, []string) {
	if strings.TrimSpace(rawSide) == "" {
		return "", nil
	}
	return sanitizeAnkiHTMLAndExtractSounds(rawSide, css)
}

func collectSoundTags(raw string) []string {
	matches := soundTagRe.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

func lookupTemplateKey(
	key string,
	fields map[string]string,
	fieldsLower map[string]string,
	special map[string]string,
) (string, bool) {
	trimmed := strings.TrimSpace(key)
	if v, ok := fields[trimmed]; ok {
		return v, true
	}
	if v, ok := special[trimmed]; ok {
		return v, true
	}
	if v, ok := fieldsLower[strings.ToLower(trimmed)]; ok {
		return v, true
	}
	for sk, sv := range special {
		if strings.EqualFold(sk, trimmed) {
			return sv, true
		}
	}
	return "", false
}

func evaluateMustacheTemplate(
	tpl string,
	fields map[string]string,
	fieldsLower map[string]string,
	special map[string]string,
	ord int,
	isBack bool,
) string {
	src := htmlCommentRe.ReplaceAllString(tpl, "")
	return evalMustacheSegment(src, fields, fieldsLower, special, ord, isBack)
}

func evalMustacheSegment(
	src string,
	fields map[string]string,
	fieldsLower map[string]string,
	special map[string]string,
	ord int,
	isBack bool,
) string {
	var out strings.Builder
	i := 0
	for i < len(src) {
		openIdx := strings.Index(src[i:], "{{")
		if openIdx < 0 {
			out.WriteString(src[i:])
			break
		}
		out.WriteString(src[i : i+openIdx])
		tagStart := i + openIdx + 2
		closeRel := strings.Index(src[tagStart:], "}}")
		if closeRel < 0 {
			out.WriteString(src[i+openIdx:])
			break
		}
		tagEnd := tagStart + closeRel
		rawTag := strings.TrimSpace(src[tagStart:tagEnd])
		afterTag := tagEnd + 2

		if strings.HasPrefix(rawTag, "#") || strings.HasPrefix(rawTag, "^") {
			isNegated := strings.HasPrefix(rawTag, "^")
			secKey := strings.TrimSpace(rawTag[1:])
			// Strip optional filter prefix on section key if user wrote {{#furigana:Field}}
			if _, ok := lookupTemplateKey(secKey, fields, fieldsLower, special); !ok {
				if colonIdx := strings.LastIndex(secKey, ":"); colonIdx >= 0 {
					secKey = strings.TrimSpace(secKey[colonIdx+1:])
				}
			}
			bodyEnd, nextPos, foundClose := findMustacheSectionEnd(src, afterTag, strings.TrimSpace(rawTag[1:]))
			if !foundClose {
				i = afterTag
				continue
			}
			val, _ := lookupTemplateKey(secKey, fields, fieldsLower, special)
			truthy := strings.TrimSpace(val) != ""
			if (!isNegated && truthy) || (isNegated && !truthy) {
				out.WriteString(evalMustacheSegment(src[afterTag:bodyEnd], fields, fieldsLower, special, ord, isBack))
			}
			i = nextPos
			continue
		}

		if strings.HasPrefix(rawTag, "/") || strings.HasPrefix(rawTag, "!") {
			i = afterTag
			continue
		}

		out.WriteString(expandMustacheTag(rawTag, fields, fieldsLower, special, ord, isBack))
		i = afterTag
	}
	return out.String()
}

func findMustacheSectionEnd(src string, start int, key string) (bodyEnd int, nextPos int, ok bool) {
	depth := 1
	i := start
	for i < len(src) {
		openIdx := strings.Index(src[i:], "{{")
		if openIdx < 0 {
			return len(src), len(src), false
		}
		tagStart := i + openIdx + 2
		closeRel := strings.Index(src[tagStart:], "}}")
		if closeRel < 0 {
			return len(src), len(src), false
		}
		tagEnd := tagStart + closeRel
		rawTag := strings.TrimSpace(src[tagStart:tagEnd])
		afterTag := tagEnd + 2

		if strings.HasPrefix(rawTag, "#") || strings.HasPrefix(rawTag, "^") {
			innerKey := strings.TrimSpace(rawTag[1:])
			if innerKey == key {
				depth++
			}
		} else if strings.HasPrefix(rawTag, "/") {
			innerKey := strings.TrimSpace(rawTag[1:])
			if innerKey == key {
				depth--
				if depth == 0 {
					return i + openIdx, afterTag, true
				}
			}
		}
		i = afterTag
	}
	return len(src), len(src), false
}

func expandMustacheTag(
	rawTag string,
	fields map[string]string,
	fieldsLower map[string]string,
	special map[string]string,
	ord int,
	isBack bool,
) string {
	if strings.EqualFold(rawTag, "FrontSide") {
		return ""
	}
	// Exact field/special name match first (in case a field name contains ':').
	if val, ok := lookupTemplateKey(rawTag, fields, fieldsLower, special); ok {
		return val
	}

	// Parse filter chain (e.g. "furigana:Field" or "furigana:text:Field")
	parts := strings.Split(rawTag, ":")
	if len(parts) == 1 {
		return ""
	}
	fieldKey := strings.TrimSpace(parts[len(parts)-1])
	filters := parts[:len(parts)-1]

	// Check if any filter is "type" or "tts" -> always suppress
	for _, f := range filters {
		fl := strings.ToLower(strings.TrimSpace(f))
		if fl == "type" || fl == "tts" {
			return ""
		}
		if fl == "hint" && !isBack {
			return ""
		}
	}

	val, ok := lookupTemplateKey(fieldKey, fields, fieldsLower, special)
	if !ok {
		return ""
	}

	// Apply filters from innermost (rightmost) to outermost (leftmost)
	for idx := len(filters) - 1; idx >= 0; idx-- {
		fl := strings.ToLower(strings.TrimSpace(filters[idx]))
		switch fl {
		case "furigana":
			val = convertBracketFurigana(val)
		case "kana":
			val = extractBracketKana(val)
		case "kanji":
			val = stripBracketKana(val)
		case "text":
			val = stripTagRe.ReplaceAllString(val, "")
		case "cloze":
			val = renderClozeText(val, ord+1, isBack)
		case "hint":
			// Front already returned ""; on Back pass value through.
		}
	}
	return val
}

func convertBracketFurigana(s string) string {
	return furiganaRe.ReplaceAllStringFunc(s, func(match string) string {
		m := furiganaRe.FindStringSubmatch(match)
		if len(m) < 3 {
			return match
		}
		return "<ruby>" + html.EscapeString(m[1]) + "<rt>" + html.EscapeString(m[2]) + "</rt></ruby>"
	})
}

func extractBracketKana(s string) string {
	return furiganaRe.ReplaceAllString(s, "$2")
}

func stripBracketKana(s string) string {
	return furiganaRe.ReplaceAllString(s, "$1")
}

func renderClozeText(s string, targetOrd int, isBack bool) string {
	return clozeTagRe.ReplaceAllStringFunc(s, func(match string) string {
		m := clozeTagRe.FindStringSubmatch(match)
		if len(m) < 3 {
			return match
		}
		num := 0
		for _, ch := range m[1] {
			num = num*10 + int(ch-'0')
		}
		answer := m[2]
		hint := ""
		if len(m) >= 4 {
			hint = m[3]
		}
		if isBack || num != targetOrd {
			return answer
		}
		if strings.TrimSpace(hint) != "" {
			return "[" + strings.TrimSpace(hint) + "]"
		}
		return "[...]"
	})
}

type cssCompoundSelector struct {
	tag     string
	id      string
	classes []string
}

type cssSelectorPart struct {
	compound    cssCompoundSelector
	directChild bool
}

type hiddenSelectors struct {
	chains [][]cssSelectorPart
}

func parseHiddenSelectors(css string) hiddenSelectors {
	var hs hiddenSelectors
	if strings.TrimSpace(css) == "" {
		return hs
	}
	noComments := cssCommentRe.ReplaceAllString(css, "")

	depth := 0
	preludeStart := 0
	bodyStart := -1
	skipAtRule := false
	var inQuote byte

	for i := 0; i < len(noComments); i++ {
		ch := noComments[i]
		if inQuote != 0 {
			if ch == '\\' && i+1 < len(noComments) {
				i++
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			continue
		}

		switch ch {
		case ';':
			if depth == 0 {
				preludeStart = i + 1
			}
		case '{':
			if depth == 0 {
				prelude := strings.TrimSpace(noComments[preludeStart:i])
				skipAtRule = strings.HasPrefix(prelude, "@")
				bodyStart = i + 1
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 {
					if !skipAtRule && bodyStart > 0 && bodyStart <= i {
						prelude := strings.TrimSpace(noComments[preludeStart : bodyStart-1])
						decl := noComments[bodyStart:i]
						if hiddenDeclRe.MatchString(decl) {
							for _, sel := range strings.Split(prelude, ",") {
								if chain, ok := parseSimpleSelectorChain(sel); ok {
									hs.chains = append(hs.chains, chain)
								}
							}
						}
					}
					preludeStart = i + 1
					bodyStart = -1
					skipAtRule = false
				}
			} else {
				preludeStart = i + 1
			}
		}
	}
	return hs
}

func parseSimpleSelectorChain(sel string) ([]cssSelectorPart, bool) {
	s := strings.TrimSpace(sel)
	if s == "" || strings.ContainsAny(s, ":[]()+~*") {
		return nil, false
	}
	tokens := strings.Fields(strings.ReplaceAll(s, ">", " > "))
	if len(tokens) == 0 {
		return nil, false
	}

	chain := make([]cssSelectorPart, 0, len(tokens))
	directChild := false
	for _, tok := range tokens {
		if tok == ">" {
			if len(chain) == 0 || directChild {
				return nil, false
			}
			directChild = true
			continue
		}
		comp, ok := parseCompoundSelector(tok)
		if !ok {
			return nil, false
		}
		chain = append(chain, cssSelectorPart{
			compound:    comp,
			directChild: directChild,
		})
		directChild = false
	}
	if directChild || len(chain) == 0 {
		return nil, false
	}
	last := chain[len(chain)-1].compound
	if last.id == "" && len(last.classes) == 0 {
		return nil, false
	}
	return chain, true
}

func parseCompoundSelector(tok string) (cssCompoundSelector, bool) {
	var comp cssCompoundSelector
	if tok == "" {
		return comp, false
	}
	i := 0
	if tok[0] != '.' && tok[0] != '#' {
		for i < len(tok) && tok[i] != '.' && tok[i] != '#' {
			i++
		}
		tag := strings.ToLower(tok[:i])
		if tag == "" {
			return comp, false
		}
		for _, r := range tag {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
				return comp, false
			}
		}
		comp.tag = tag
	}
	for i < len(tok) {
		kind := tok[i]
		i++
		start := i
		for i < len(tok) && tok[i] != '.' && tok[i] != '#' {
			i++
		}
		name := tok[start:i]
		if name == "" {
			return comp, false
		}
		if kind == '.' {
			comp.classes = append(comp.classes, name)
		} else {
			if comp.id != "" && comp.id != name {
				return comp, false
			}
			comp.id = name
		}
	}
	if comp.tag == "" && comp.id == "" && len(comp.classes) == 0 {
		return comp, false
	}
	return comp, true
}

func matchesCompoundSelector(n *xhtml.Node, c cssCompoundSelector) bool {
	if n == nil || n.Type != xhtml.ElementNode {
		return false
	}
	if c.tag != "" && strings.ToLower(n.Data) != c.tag {
		return false
	}
	if c.id == "" && len(c.classes) == 0 {
		return true
	}
	var idVal, classVal string
	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "id":
			idVal = attr.Val
		case "class":
			classVal = attr.Val
		}
	}
	if c.id != "" && strings.TrimSpace(idVal) != c.id {
		return false
	}
	if len(c.classes) > 0 {
		nodeClasses := strings.Fields(classVal)
		for _, req := range c.classes {
			found := false
			for _, nc := range nodeClasses {
				if nc == req {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func matchesChainAt(n *xhtml.Node, chain []cssSelectorPart, idx int) bool {
	if n == nil || n.Type != xhtml.ElementNode || !matchesCompoundSelector(n, chain[idx].compound) {
		return false
	}
	if idx == 0 {
		return true
	}
	if chain[idx].directChild {
		return matchesChainAt(n.Parent, chain, idx-1)
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == xhtml.ElementNode && matchesChainAt(p, chain, idx-1) {
			return true
		}
	}
	return false
}

func isElementHidden(n *xhtml.Node, hs hiddenSelectors) bool {
	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)
		switch key {
		case "hidden":
			return true
		case "style":
			if hiddenDeclRe.MatchString(attr.Val) {
				return true
			}
		}
	}
	for _, chain := range hs.chains {
		if matchesChainAt(n, chain, len(chain)-1) {
			return true
		}
	}
	return false
}

var skipSubtreeTags = map[string]bool{
	"style":    true,
	"script":   true,
	"head":     true,
	"noscript": true,
	"svg":      true,
	"iframe":   true,
	"object":   true,
	"canvas":   true,
	"template": true,
	"button":   true,
}

var blockBreakTags = map[string]bool{
	"div": true, "p": true, "li": true, "tr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "article": true, "header": true, "footer": true, "main": true,
	"blockquote": true, "details": true, "summary": true,
	"table": true, "ul": true, "ol": true, "pre": true,
}

// SanitizeAnkiHTML strips CSS, scripts, hidden elements, and general HTML tags while preserving
// <br/> line breaks and <ruby>/<rt>/<rp> furigana markup with zero attributes.
func SanitizeAnkiHTML(rawHTML, css string) string {
	sanitized, _ := sanitizeAnkiHTMLInternal(rawHTML, css, false)
	return sanitized
}

func sanitizeAnkiHTMLAndExtractSounds(rawHTML, css string) (string, []string) {
	return sanitizeAnkiHTMLInternal(rawHTML, css, true)
}

func sanitizeAnkiHTMLInternal(rawHTML, css string, extractAndStripSounds bool) (string, []string) {
	if strings.TrimSpace(rawHTML) == "" {
		return "", nil
	}
	hs := parseHiddenSelectors(css)

	doc, err := xhtml.Parse(strings.NewReader("<!DOCTYPE html><html><body>" + rawHTML + "</body></html>"))
	if err != nil {
		plain := stripTagRe.ReplaceAllString(htmlCommentRe.ReplaceAllString(rawHTML, ""), "")
		var audio []string
		if extractAndStripSounds {
			audio = collectSoundTags(plain)
			plain = soundTagRe.ReplaceAllString(plain, "")
		}
		escaped := html.EscapeString(plain)
		return normalizeSanitizedOutput(escaped), audio
	}

	var body *xhtml.Node
	var findBody func(*xhtml.Node)
	findBody = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for c := n.FirstChild; c != nil && body == nil; c = c.NextSibling {
			findBody(c)
		}
	}
	findBody(doc)
	if body == nil {
		body = doc
	}

	var out strings.Builder
	var audio []string
	var audioSeen map[string]struct{}
	if extractAndStripSounds {
		audioSeen = make(map[string]struct{})
	}
	walkSanitizeNode(body, &out, hs, false, extractAndStripSounds, &audio, audioSeen)
	return normalizeSanitizedOutput(out.String()), audio
}

var allowedImageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".avif": true,
	".bmp":  true,
	".tif":  true,
	".tiff": true,
	".svg":  true,
}

func sanitizeAnkiImageSrc(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "vbscript:") {
		return "", false
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return trimmed, true
	}
	if strings.HasPrefix(lower, "data:image/") {
		if strings.HasPrefix(lower, "data:image/svg") {
			return "", false
		}
		return trimmed, true
	}

	normalized := strings.ReplaceAll(trimmed, `\`, "/")
	normalized = strings.TrimPrefix(normalized, "/media/")
	if strings.Contains(normalized, "..") {
		return "", false
	}
	if unescaped, err := url.PathUnescape(normalized); err == nil && unescaped != "" {
		if strings.Contains(unescaped, "..") {
			return "", false
		}
		normalized = strings.ReplaceAll(unescaped, `\`, "/")
	}
	base := strings.TrimSpace(path.Base(normalized))
	if base == "" || base == "." || base == ".." || base == "/" {
		return "", false
	}
	ext := strings.ToLower(path.Ext(base))
	if !allowedImageExtensions[ext] {
		return "", false
	}
	return "/media/" + url.PathEscape(base), true
}

func appendBreakIfNeeded(out *strings.Builder) {
	trimmed := strings.TrimRight(out.String(), " \t\r\n")
	if trimmed == "" || strings.HasSuffix(trimmed, "<br/>") {
		return
	}
	out.WriteString("<br/>")
}

func walkSanitizeNode(
	n *xhtml.Node,
	out *strings.Builder,
	hs hiddenSelectors,
	inRuby bool,
	extractAndStripSounds bool,
	audioOut *[]string,
	audioSeen map[string]struct{},
) {
	switch n.Type {
	case xhtml.TextNode:
		txt := n.Data
		if extractAndStripSounds {
			if matches := soundTagRe.FindAllStringSubmatch(txt, -1); len(matches) > 0 {
				for _, m := range matches {
					if len(m) < 2 {
						continue
					}
					name := strings.TrimSpace(m[1])
					if name == "" {
						continue
					}
					if _, ok := audioSeen[name]; !ok {
						audioSeen[name] = struct{}{}
						*audioOut = append(*audioOut, name)
					}
				}
				txt = soundTagRe.ReplaceAllString(txt, "")
			}
		}
		txt = strings.ReplaceAll(txt, "\u00a0", " ")
		if inRuby {
			out.WriteString(html.EscapeString(txt))
			return
		}
		out.WriteString(escapeTextWithFurigana(txt))
		return
	case xhtml.CommentNode, xhtml.DoctypeNode:
		return
	case xhtml.ElementNode:
		tag := strings.ToLower(n.Data)
		if skipSubtreeTags[tag] {
			return
		}
		if tag != "body" && tag != "html" && isElementHidden(n, hs) {
			return
		}

		switch tag {
		case "br":
			out.WriteString("<br/>")
			return
		case "img":
			for _, attr := range n.Attr {
				if strings.EqualFold(attr.Key, "src") {
					if safeSrc, ok := sanitizeAnkiImageSrc(attr.Val); ok {
						appendBreakIfNeeded(out)
						out.WriteString(`<img src="`)
						out.WriteString(html.EscapeString(safeSrc))
						out.WriteString(`"/>`)
						appendBreakIfNeeded(out)
					}
					break
				}
			}
			return
		case "hr":
			appendBreakIfNeeded(out)
			return
		case "ruby", "rt", "rp":
			out.WriteString("<" + tag + ">")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkSanitizeNode(c, out, hs, true, extractAndStripSounds, audioOut, audioSeen)
			}
			out.WriteString("</" + tag + ">")
			return
		case "td", "th":
			prevLen := out.Len()
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkSanitizeNode(c, out, hs, inRuby, extractAndStripSounds, audioOut, audioSeen)
			}
			if out.Len() > prevLen {
				s := out.String()
				if !strings.HasSuffix(s, " ") && !strings.HasSuffix(s, "<br/>") {
					out.WriteByte(' ')
				}
			}
			return
		}

		isBlock := blockBreakTags[tag]
		if isBlock {
			appendBreakIfNeeded(out)
		}
		prevLen := out.Len()
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkSanitizeNode(c, out, hs, inRuby, extractAndStripSounds, audioOut, audioSeen)
		}
		if isBlock && out.Len() > prevLen {
			appendBreakIfNeeded(out)
		}
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkSanitizeNode(c, out, hs, inRuby, extractAndStripSounds, audioOut, audioSeen)
		}
	}
}

func escapeTextWithFurigana(txt string) string {
	matches := furiganaRe.FindAllStringSubmatchIndex(txt, -1)
	if len(matches) == 0 {
		return html.EscapeString(txt)
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(html.EscapeString(txt[last:m[0]]))
		kanji := html.EscapeString(txt[m[2]:m[3]])
		reading := html.EscapeString(txt[m[4]:m[5]])
		b.WriteString("<ruby>")
		b.WriteString(kanji)
		b.WriteString("<rt>")
		b.WriteString(reading)
		b.WriteString("</rt></ruby>")
		last = m[1]
	}
	b.WriteString(html.EscapeString(txt[last:]))
	return b.String()
}

func normalizeSanitizedOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// Collapse newlines inside source HTML into spaces (HTML ignores raw newlines outside <pre>)
	s = strings.ReplaceAll(s, "\n", " ")
	s = horizSpaceRe.ReplaceAllString(s, " ")
	s = spaceBeforeBr.ReplaceAllString(s, "<br/>")
	s = spaceAfterBr.ReplaceAllString(s, "<br/>")
	s = multiBrRe.ReplaceAllString(s, "<br/><br/>")

	for {
		trimmed := strings.TrimSpace(s)
		trimmed = strings.TrimPrefix(trimmed, "<br/>")
		trimmed = strings.TrimSuffix(trimmed, "<br/>")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == s {
			break
		}
		s = trimmed
	}
	return s
}

// SplitAnkiConnectQuestionAnswer strips FrontSide/Question duplication across <hr id="answer">
// in AnkiConnect's pre-rendered question/answer strings and sanitizes both.
func SplitAnkiConnectQuestionAnswer(question, answer, css string) (frontHTML, backHTML string, frontAudio, backAudio, allAudio []string) {
	frontHTML, frontAudio = extractAndSanitizeSide(question, css)

	cleanedAns := extractBackTemplateBody(answer)
	backHTML, backAudio = extractAndSanitizeSide(cleanedAns, css)
	allAudio = mergeUniqueStrings(frontAudio, backAudio)
	return frontHTML, backHTML, frontAudio, backAudio, allAudio
}

// DeckFieldsInfo describes the note fields discovered in a deck and suggested defaults from its templates.
type DeckFieldsInfo struct {
	Fields       []string `json:"fields"`
	DefaultFront []string `json:"defaultFront"`
	DefaultBack  []string `json:"defaultBack"`
}

var mustacheTagExtractRe = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

// ExtractTemplateFieldNames extracts canonical field names referenced by an Anki Mustache template.
func ExtractTemplateFieldNames(tmplSrc string, canonicalByLower map[string]string) []string {
	matches := mustacheTagExtractRe.FindAllStringSubmatch(tmplSrc, -1)
	seen := make(map[string]struct{}, len(matches))
	var out []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		tag := strings.TrimSpace(m[1])
		if tag == "" || strings.HasPrefix(tag, "#") || strings.HasPrefix(tag, "/") ||
			strings.HasPrefix(tag, "^") || strings.HasPrefix(tag, "!") {
			continue
		}
		if idx := strings.LastIndex(tag, ":"); idx >= 0 {
			tag = strings.TrimSpace(tag[idx+1:])
		}
		lower := strings.ToLower(tag)
		if lower == "frontside" || lower == "tags" || lower == "deck" ||
			lower == "subdeck" || lower == "card" || lower == "type" {
			continue
		}
		canonical, ok := canonicalByLower[lower]
		if !ok {
			continue
		}
		if _, dup := seen[canonical]; !dup {
			seen[canonical] = struct{}{}
			out = append(out, canonical)
		}
	}
	return out
}

// RenderSelectedFields renders only the user-chosen Front and Back fields, stripping HTML/CSS
// while preserving line breaks, <ruby> furigana, and [sound:...] audio extraction.
func RenderSelectedFields(
	fields map[string]FieldData,
	selectedFront []string,
	selectedBack []string,
	css string,
) (frontHTML, backHTML string, frontAudio, backAudio, allAudio []string) {
	byLower := make(map[string]FieldData, len(fields))
	for k, v := range fields {
		byLower[strings.ToLower(strings.TrimSpace(k))] = v
	}

	renderList := func(names []string) (string, []string) {
		var parts []string
		var audios []string
		for _, name := range names {
			f, ok := byLower[strings.ToLower(strings.TrimSpace(name))]
			if !ok || strings.TrimSpace(f.Value) == "" {
				continue
			}
			cleaned, extracted := sanitizeAnkiHTMLAndExtractSounds(f.Value, css)
			if len(extracted) > 0 {
				audios = mergeUniqueStrings(audios, extracted)
			}
			if cleaned != "" {
				parts = append(parts, cleaned)
			}
		}
		return strings.Join(parts, "<br/>"), audios
	}

	frontHTML, frontAudio = renderList(selectedFront)
	backHTML, backAudio = renderList(selectedBack)
	allAudio = mergeUniqueStrings(frontAudio, backAudio)
	return frontHTML, backHTML, frontAudio, backAudio, allAudio
}

// Unused import guard for fmt/sort if needed by future template helpers
var _ = fmt.Sprintf
var _ = sort.Strings
