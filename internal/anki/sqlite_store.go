package anki

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sqlite "modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterCollationUtf8("unicase", func(left, right string) int {
		return strings.Compare(strings.ToLower(left), strings.ToLower(right))
	})
}

var (
	isAnkiRunningFunc = defaultIsAnkiRunning
	colCrtCacheMu     sync.RWMutex
	colCrtCache       = make(map[string]int64)
)

// cacheCollectionCRT stores the collection creation timestamp (crt) for fast lock-free day rollovers.
func cacheCollectionCRT(collectionPath string, crt int64) {
	if collectionPath == "" || crt <= 0 {
		return
	}
	colCrtCacheMu.Lock()
	colCrtCache[collectionPath] = crt
	colCrtCacheMu.Unlock()
}

func getCachedCollectionCRT(collectionPath string) (int64, bool) {
	if collectionPath == "" {
		return 0, false
	}
	colCrtCacheMu.RLock()
	crt, ok := colCrtCache[collectionPath]
	colCrtCacheMu.RUnlock()
	return crt, ok
}

func computeCollectionDay(crt int64) string {
	nowSec := time.Now().Unix()
	today := (nowSec - crt) / 86400
	if today < 0 {
		today = 0
	}
	dayStart := time.Unix(crt+today*86400, 0).Local()
	return dayStart.Format("2006-01-02")
}

// IsAnkiDesktopRunning reports whether an Anki Desktop process (anki.exe on Windows) is active.
func IsAnkiDesktopRunning() bool {
	return isAnkiRunningFunc()
}

type sqliteDeckEntry struct {
	id     int64
	name   string
	common deckCommon
	kind   deckKindInfo
}

type deckLimitCfg struct {
	newPerDay     uint32
	reviewsPerDay uint32
}

func openSQLiteWithTimeout(collectionPath string, readOnly bool, busyTimeoutMs int) (*sql.DB, error) {
	if strings.TrimSpace(collectionPath) == "" {
		return nil, errors.New("collection.anki2 path is empty")
	}
	db, err := sql.Open("sqlite", collectionPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", collectionPath, err)
	}
	db.SetMaxOpenConns(1)

	if busyTimeoutMs > 0 {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d;", busyTimeoutMs)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set busy_timeout: %w", err)
		}
	}
	_, _ = db.Exec("PRAGMA cache_size = -512;")
	_, _ = db.Exec("PRAGMA mmap_size = 0;")
	if readOnly {
		if _, err := db.Exec("PRAGMA query_only = ON;"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set query_only: %w", err)
		}
	}
	return db, nil
}

func openSQLite(collectionPath string, readOnly bool) (*sql.DB, error) {
	// Keep read-only probes snappy (300ms) so UI never stalls 3-12s if Anki holds a lock,
	// while retaining a longer 3000ms busy timeout for write transactions.
	timeout := 3000
	if readOnly {
		timeout = 300
	}
	return openSQLiteWithTimeout(collectionPath, readOnly, timeout)
}

type queryExecutor interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

func tableColumns(q queryExecutor, table string) map[string]bool {
	cols := make(map[string]bool)
	rows, err := q.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return cols
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
			cols[strings.ToLower(name)] = true
		}
	}
	return cols
}

func readCollectionMeta(db *sql.DB) (crt int64, ver int, err error) {
	row := db.QueryRow("SELECT crt, ver FROM col LIMIT 1")
	if err := row.Scan(&crt, &ver); err != nil {
		return 0, 0, fmt.Errorf("read col metadata: %w", err)
	}
	return crt, ver, nil
}

func readCollapseTime(db *sql.DB) int64 {
	var raw string
	if err := db.QueryRow("SELECT val FROM config WHERE key = 'collapseTime' LIMIT 1").Scan(&raw); err == nil {
		if v, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); parseErr == nil && v >= 0 {
			return v
		}
	}
	return 1200
}

// SQLiteCollectionDay returns the current study day ("YYYY-MM-DD") aligned with Anki's col.crt rollover hour.
func SQLiteCollectionDay(collectionPath string) (string, error) {
	if crt, ok := getCachedCollectionCRT(collectionPath); ok {
		return computeCollectionDay(crt), nil
	}
	if IsAnkiDesktopRunning() {
		return "", errors.New("anki desktop is running; skipping blocking SQLite day probe")
	}
	db, err := openSQLiteWithTimeout(collectionPath, true, 150)
	if err != nil {
		return "", err
	}
	defer db.Close()

	crt, _, err := readCollectionMeta(db)
	if err != nil {
		return "", err
	}
	cacheCollectionCRT(collectionPath, crt)
	return computeCollectionDay(crt), nil
}

func loadSQLiteDecks(db *sql.DB) ([]sqliteDeckEntry, error) {
	deckCols := tableColumns(db, "decks")
	if deckCols["id"] && deckCols["name"] {
		query := "SELECT id, name FROM decks"
		hasProto := deckCols["common"] && deckCols["kind"]
		if hasProto {
			query = "SELECT id, name, common, kind FROM decks"
		}
		rows, err := db.Query(query)
		if err == nil {
			defer rows.Close()
			var decks []sqliteDeckEntry
			for rows.Next() {
				var id int64
				var rawName string
				var commonBytes, kindBytes []byte
				if hasProto {
					if scanErr := rows.Scan(&id, &rawName, &commonBytes, &kindBytes); scanErr != nil {
						return nil, scanErr
					}
				} else {
					if scanErr := rows.Scan(&id, &rawName); scanErr != nil {
						return nil, scanErr
					}
				}
				entry := sqliteDeckEntry{
					id:   id,
					name: strings.ReplaceAll(rawName, "\x1f", "::"),
				}
				if len(commonBytes) > 0 {
					entry.common = decodeDeckCommon(commonBytes)
				}
				if len(kindBytes) > 0 {
					entry.kind = decodeDeckKind(kindBytes)
				} else {
					entry.kind = deckKindInfo{ConfigID: 1}
				}
				decks = append(decks, entry)
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				return nil, rowsErr
			}
			sort.Slice(decks, func(i, j int) bool {
				return strings.ToLower(decks[i].name) < strings.ToLower(decks[j].name)
			})
			return decks, nil
		}
	}

	// Fallback for legacy Schema v11 (JSON stored in col.decks)
	var decksJSON string
	if colErr := db.QueryRow("SELECT decks FROM col LIMIT 1").Scan(&decksJSON); colErr != nil {
		return nil, fmt.Errorf("load decks (table & legacy fallback failed): %w", colErr)
	}
	var rawMap map[string]struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if unmarshalErr := json.Unmarshal([]byte(decksJSON), &rawMap); unmarshalErr != nil {
		return nil, fmt.Errorf("parse legacy col.decks json: %w", unmarshalErr)
	}
	decks := make([]sqliteDeckEntry, 0, len(rawMap))
	for _, d := range rawMap {
		decks = append(decks, sqliteDeckEntry{
			id:   d.ID,
			name: strings.ReplaceAll(d.Name, "\x1f", "::"),
			kind: deckKindInfo{ConfigID: 1},
		})
	}
	sort.Slice(decks, func(i, j int) bool {
		return strings.ToLower(decks[i].name) < strings.ToLower(decks[j].name)
	})
	return decks, nil
}

func loadSQLiteDeckConfigs(db *sql.DB) map[int64]deckLimitCfg {
	res := make(map[int64]deckLimitCfg)
	rows, err := db.Query("SELECT id, config FROM deck_config")
	if err != nil {
		return res
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var cfgBytes []byte
		if scanErr := rows.Scan(&id, &cfgBytes); scanErr == nil {
			n, r := decodeDeckConfigLimits(cfgBytes)
			res[id] = deckLimitCfg{newPerDay: n, reviewsPerDay: r}
		}
	}
	return res
}

func deckRemainingQuota(d sqliteDeckEntry, configs map[int64]deckLimitCfg, today int64) (remNew int, remRev int) {
	const unlimited = 1 << 29
	if len(configs) == 0 || d.kind.IsFiltered {
		return unlimited, unlimited
	}
	cfg, ok := configs[d.kind.ConfigID]
	if !ok {
		return unlimited, unlimited
	}

	newLim := int(cfg.newPerDay)
	revLim := int(cfg.reviewsPerDay)

	if d.kind.NewLimitToday != nil && int64(d.kind.NewLimitToday.Today) == today {
		newLim = int(d.kind.NewLimitToday.Limit)
	} else if d.kind.NewLimit != nil {
		newLim = int(*d.kind.NewLimit)
	}

	if d.kind.ReviewLimitToday != nil && int64(d.kind.ReviewLimitToday.Today) == today {
		revLim = int(d.kind.ReviewLimitToday.Limit)
	} else if d.kind.ReviewLimit != nil {
		revLim = int(*d.kind.ReviewLimit)
	}

	newStudied := 0
	revStudied := 0
	if int64(d.common.LastDayStudied) == today {
		if d.common.NewStudied > 0 {
			newStudied = int(d.common.NewStudied)
		}
		if d.common.ReviewStudied > 0 {
			revStudied = int(d.common.ReviewStudied)
		}
	}

	remNew = newLim - newStudied
	if remNew < 0 {
		remNew = 0
	}
	remRev = revLim - revStudied
	if remRev < 0 {
		remRev = 0
	}
	return remNew, remRev
}

type deckCardCounts struct {
	newCount int
	revCount int
	lrnCount int
}

type deckTreeCounts struct {
	subtreeNew int
	subtreeRev int
	subtreeLrn int
	canDrawNew bool
	canDrawRev bool
}

func computeDeckTreeCounts(
	decks []sqliteDeckEntry,
	configs map[int64]deckLimitCfg,
	rawByDeck map[int64]deckCardCounts,
	today int64,
) map[int64]deckTreeCounts {
	byName := make(map[string]sqliteDeckEntry, len(decks))
	remNewByID := make(map[int64]int, len(decks))
	remRevByID := make(map[int64]int, len(decks))
	childrenByParent := make(map[int64][]int64, len(decks))

	for _, d := range decks {
		byName[d.name] = d
		rn, rr := deckRemainingQuota(d, configs, today)
		remNewByID[d.id] = rn
		remRevByID[d.id] = rr
	}

	for _, d := range decks {
		parentName := deckParentName(d.name)
		for parentName != "" {
			if p, ok := byName[parentName]; ok {
				childrenByParent[p.id] = append(childrenByParent[p.id], d.id)
				break
			}
			parentName = deckParentName(parentName)
		}
	}

	// Sort decks deepest-first so children aggregate before parents.
	ordered := make([]sqliteDeckEntry, len(decks))
	copy(ordered, decks)
	sort.Slice(ordered, func(i, j int) bool {
		di := strings.Count(ordered[i].name, "::")
		dj := strings.Count(ordered[j].name, "::")
		if di != dj {
			return di > dj
		}
		return ordered[i].name < ordered[j].name
	})

	out := make(map[int64]deckTreeCounts, len(decks))
	for _, d := range ordered {
		raw := rawByDeck[d.id]
		sNew := raw.newCount
		sRev := raw.revCount
		sLrn := raw.lrnCount
		for _, childID := range childrenByParent[d.id] {
			c := out[childID]
			sNew += c.subtreeNew
			sRev += c.subtreeRev
			sLrn += c.subtreeLrn
		}
		if sNew > remNewByID[d.id] {
			sNew = remNewByID[d.id]
		}
		if sRev > remRevByID[d.id] {
			sRev = remRevByID[d.id]
		}
		out[d.id] = deckTreeCounts{
			subtreeNew: sNew,
			subtreeRev: sRev,
			subtreeLrn: sLrn,
		}
	}

	// Determine whether each deck (and all its existing ancestors) still has >0 daily quota.
	for _, d := range decks {
		canNew := remNewByID[d.id] > 0
		canRev := remRevByID[d.id] > 0
		parentName := deckParentName(d.name)
		for parentName != "" && (canNew || canRev) {
			if p, ok := byName[parentName]; ok {
				if remNewByID[p.id] <= 0 {
					canNew = false
				}
				if remRevByID[p.id] <= 0 {
					canRev = false
				}
			}
			parentName = deckParentName(parentName)
		}
		cur := out[d.id]
		cur.canDrawNew = canNew
		cur.canDrawRev = canRev
		out[d.id] = cur
	}
	return out
}

func deckParentName(name string) string {
	idx := strings.LastIndex(name, "::")
	if idx < 0 {
		return ""
	}
	return name[:idx]
}

func loadSQLiteFields(db *sql.DB, modelID int64) ([]string, error) {
	rows, err := db.Query("SELECT name FROM fields WHERE ntid = ? ORDER BY ord", modelID)
	if err == nil {
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name string
			if scanErr := rows.Scan(&name); scanErr != nil {
				return nil, scanErr
			}
			names = append(names, name)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return nil, rowsErr
		}
		if len(names) > 0 {
			return names, nil
		}
	}

	// Fallback for legacy Schema v11 (JSON stored in col.models)
	var modelsJSON string
	if colErr := db.QueryRow("SELECT models FROM col LIMIT 1").Scan(&modelsJSON); colErr != nil {
		return nil, fmt.Errorf("load fields for model %d (fields err=%v): %w", modelID, err, colErr)
	}
	if strings.TrimSpace(modelsJSON) == "" {
		return nil, fmt.Errorf("load fields for model %d: fields table query failed (%v) and col.models is empty", modelID, err)
	}
	var rawModels map[string]struct {
		Flds []struct {
			Name string `json:"name"`
			Ord  int    `json:"ord"`
		} `json:"flds"`
	}
	if unmarshalErr := json.Unmarshal([]byte(modelsJSON), &rawModels); unmarshalErr != nil {
		return nil, fmt.Errorf("parse legacy col.models json: %w", unmarshalErr)
	}
	model, ok := rawModels[fmt.Sprintf("%d", modelID)]
	if !ok {
		return nil, nil
	}
	sort.Slice(model.Flds, func(i, j int) bool {
		return model.Flds[i].Ord < model.Flds[j].Ord
	})
	names := make([]string, 0, len(model.Flds))
	for _, f := range model.Flds {
		names = append(names, f.Name)
	}
	return names, nil
}

func loadSQLiteTemplate(db *sql.DB, modelID int64, cardOrd int) (tmpl CardTemplate, modelName string, ok bool) {
	// Schema v18: notetypes + templates tables
	ntCols := tableColumns(db, "notetypes")
	tmplCols := tableColumns(db, "templates")
	if ntCols["id"] && ntCols["name"] && tmplCols["ntid"] && tmplCols["config"] {
		var ntCfg []byte
		if ntCols["config"] {
			_ = db.QueryRow("SELECT name, config FROM notetypes WHERE id = ?", modelID).Scan(&modelName, &ntCfg)
		} else {
			_ = db.QueryRow("SELECT name FROM notetypes WHERE id = ?", modelID).Scan(&modelName)
		}
		isCloze, css := decodeNotetypeConfig(ntCfg)

		var tName string
		var tCfg []byte
		err := db.QueryRow(
			"SELECT name, config FROM templates WHERE ntid = ? AND ord = ? LIMIT 1",
			modelID, cardOrd,
		).Scan(&tName, &tCfg)
		if err != nil && isCloze {
			// Cloze / Image Occlusion notes have multiple card ordinals sharing template ord=0.
			err = db.QueryRow(
				"SELECT name, config FROM templates WHERE ntid = ? AND ord = 0 LIMIT 1",
				modelID,
			).Scan(&tName, &tCfg)
		}
		if err == nil {
			qfmt, afmt := decodeTemplateConfig(tCfg)
			if qfmt != "" || afmt != "" {
				return CardTemplate{
					Ord:     cardOrd,
					Name:    tName,
					Qfmt:    qfmt,
					Afmt:    afmt,
					CSS:     css,
					IsCloze: isCloze,
				}, modelName, true
			}
		}
	}

	// Legacy Schema v11 fallback (col.models JSON)
	var modelsJSON string
	if colErr := db.QueryRow("SELECT models FROM col LIMIT 1").Scan(&modelsJSON); colErr != nil {
		return CardTemplate{}, modelName, false
	}
	var rawModels map[string]struct {
		Name  string `json:"name"`
		CSS   string `json:"css"`
		Type  int    `json:"type"`
		Tmpls []struct {
			Name string `json:"name"`
			Ord  int    `json:"ord"`
			Qfmt string `json:"qfmt"`
			Afmt string `json:"afmt"`
		} `json:"tmpls"`
	}
	if unmarshalErr := json.Unmarshal([]byte(modelsJSON), &rawModels); unmarshalErr != nil {
		return CardTemplate{}, modelName, false
	}
	model, exists := rawModels[fmt.Sprintf("%d", modelID)]
	if !exists {
		return CardTemplate{}, modelName, false
	}
	isCloze := model.Type == 1
	targetOrd := cardOrd
	if isCloze {
		targetOrd = 0
	}
	for _, t := range model.Tmpls {
		if t.Ord == targetOrd || (len(model.Tmpls) == 1 && targetOrd == 0) {
			return CardTemplate{
				Ord:     cardOrd,
				Name:    t.Name,
				Qfmt:    t.Qfmt,
				Afmt:    t.Afmt,
				CSS:     model.CSS,
				IsCloze: isCloze,
			}, model.Name, true
		}
	}
	return CardTemplate{}, model.Name, false
}

func filterDeckIDs(decks []sqliteDeckEntry, selectedDeck string) []int64 {
	target := strings.TrimSpace(selectedDeck)
	ids := make([]int64, 0, len(decks))
	for _, d := range decks {
		if target == "" || d.name == target || strings.HasPrefix(d.name, target+"::") {
			ids = append(ids, d.id)
		}
	}
	return ids
}

func buildInClause(ids []int64) (string, []interface{}) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return "(" + strings.Join(placeholders, ",") + ")", args
}

// SQLiteDeckNames reads deck names directly from Anki's collection.anki2 SQLite database.
func SQLiteDeckNames(collectionPath string, onlyDue bool) ([]string, error) {
	db, err := openSQLite(collectionPath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	crt, _, err := readCollectionMeta(db)
	if err != nil {
		return nil, err
	}
	cacheCollectionCRT(collectionPath, crt)
	decks, err := loadSQLiteDecks(db)
	if err != nil {
		return nil, err
	}

	allNames := make([]string, 0, len(decks))
	for _, d := range decks {
		allNames = append(allNames, d.name)
	}
	sort.Strings(allNames)

	if !onlyDue || len(decks) == 0 {
		return allNames, nil
	}

	nowSec := time.Now().Unix()
	collapseTime := readCollapseTime(db)
	today := (nowSec - crt) / 86400
	if today < 0 {
		today = 0
	}

	rawByDeck := loadRawCardCountsByDeck(db, today, nowSec+collapseTime)
	configs := loadSQLiteDeckConfigs(db)
	treeCounts := computeDeckTreeCounts(decks, configs, rawByDeck, today)

	var dueNames []string
	for _, d := range decks {
		c := treeCounts[d.id]
		if c.subtreeNew+c.subtreeRev+c.subtreeLrn > 0 {
			dueNames = append(dueNames, d.name)
		}
	}

	if len(dueNames) == 0 {
		return allNames, nil
	}
	sort.Strings(dueNames)
	return dueNames, nil
}

func loadRawCardCountsByDeck(db *sql.DB, today int64, learnCutoff int64) map[int64]deckCardCounts {
	out := make(map[int64]deckCardCounts)
	rows, err := db.Query(
		`SELECT did,
		        SUM(CASE WHEN queue = 0 THEN 1 ELSE 0 END),
		        SUM(CASE WHEN queue = 2 AND due <= ? THEN 1 ELSE 0 END),
		        SUM(CASE WHEN (queue = 1 AND due <= ?) OR (queue = 3 AND due <= ?) THEN 1 ELSE 0 END)
		 FROM cards
		 WHERE queue >= 0
		 GROUP BY did`,
		today, learnCutoff, today,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var did int64
		var n, r, l int
		if scanErr := rows.Scan(&did, &n, &r, &l); scanErr == nil {
			out[did] = deckCardCounts{newCount: n, revCount: r, lrnCount: l}
		}
	}
	return out
}

// SQLiteFetchNextCard fetches the next due (prioritized) or new card directly from collection.anki2.
func SQLiteFetchNextCard(collectionPath string, selectedDeck string) (*ParsedCard, error) {
	return SQLiteFetchNextCardWithFields(collectionPath, selectedDeck, nil, nil)
}

// SQLiteGetDeckFields inspects all note types in the selected deck (or all decks) and returns
// the ordered unique field names plus suggested Front/Back defaults extracted from their templates.
func SQLiteGetDeckFields(collectionPath string, selectedDeck string) (*DeckFieldsInfo, error) {
	db, err := openSQLite(collectionPath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	decks, err := loadSQLiteDecks(db)
	if err != nil {
		return nil, err
	}
	matchingIDs := filterDeckIDs(decks, selectedDeck)
	if len(matchingIDs) == 0 {
		return &DeckFieldsInfo{
			Fields:       []string{},
			DefaultFront: []string{},
			DefaultBack:  []string{},
		}, nil
	}

	inAll, allArgs := buildInClause(matchingIDs)
	rows, err := db.Query(
		fmt.Sprintf(
			`SELECT DISTINCT n.mid, c.ord
			 FROM cards c
			 JOIN notes n ON c.nid = n.id
			 WHERE c.did IN %s`,
			inAll,
		),
		allArgs...,
	)
	if err != nil {
		return nil, fmt.Errorf("query deck notetypes: %w", err)
	}
	defer rows.Close()

	type modelOrd struct {
		mid int64
		ord int
	}
	var pairs []modelOrd
	for rows.Next() {
		var p modelOrd
		if scanErr := rows.Scan(&p.mid, &p.ord); scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan deck notetype: %w", scanErr)
		}
		pairs = append(pairs, p)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	seenFields := make(map[string]bool)
	seenFront := make(map[string]bool)
	seenBack := make(map[string]bool)
	info := &DeckFieldsInfo{
		Fields:       []string{},
		DefaultFront: []string{},
		DefaultBack:  []string{},
	}

	for _, p := range pairs {
		fNames, fErr := loadSQLiteFields(db, p.mid)
		if fErr != nil {
			return nil, fmt.Errorf("load fields for model %d: %w", p.mid, fErr)
		}
		canonicalByLower := make(map[string]string, len(fNames))
		for _, fn := range fNames {
			canonicalByLower[strings.ToLower(strings.TrimSpace(fn))] = fn
			if !seenFields[fn] {
				seenFields[fn] = true
				info.Fields = append(info.Fields, fn)
			}
		}
		if tmpl, _, hasTmpl := loadSQLiteTemplate(db, p.mid, p.ord); hasTmpl {
			for _, f := range ExtractTemplateFieldNames(tmpl.Qfmt, canonicalByLower) {
				if !seenFront[f] {
					seenFront[f] = true
					info.DefaultFront = append(info.DefaultFront, f)
				}
			}
			for _, f := range ExtractTemplateFieldNames(extractBackTemplateBody(tmpl.Afmt), canonicalByLower) {
				if !seenBack[f] {
					seenBack[f] = true
					info.DefaultBack = append(info.DefaultBack, f)
				}
			}
		}
	}

	if len(info.DefaultFront) == 0 && len(info.Fields) > 0 {
		info.DefaultFront = []string{info.Fields[0]}
	}
	if len(info.DefaultBack) == 0 && len(info.Fields) > 1 {
		info.DefaultBack = append(info.DefaultBack, info.Fields[1:]...)
	}
	return info, nil
}

// SQLiteFetchNextCardWithFields fetches the next due/new card and renders user-selected Front/Back fields if provided.
func SQLiteFetchNextCardWithFields(collectionPath string, selectedDeck string, frontFields, backFields []string) (*ParsedCard, error) {
	db, err := openSQLite(collectionPath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	crt, _, err := readCollectionMeta(db)
	if err != nil {
		return nil, err
	}
	cacheCollectionCRT(collectionPath, crt)
	decks, err := loadSQLiteDecks(db)
	if err != nil {
		return nil, err
	}

	deckNamesByID := make(map[int64]string, len(decks))
	decksByName := make(map[string]sqliteDeckEntry, len(decks))
	for _, d := range decks {
		deckNamesByID[d.id] = d.name
		decksByName[d.name] = d
	}

	matchingIDs := filterDeckIDs(decks, selectedDeck)
	if len(matchingIDs) == 0 {
		return &ParsedCard{
			DeckName:       selectedDeck,
			RemainingDue:   0,
			RemainingNew:   0,
			TotalRemaining: 0,
			SourceMode:     "sqlite",
		}, nil
	}

	nowSec := time.Now().Unix()
	collapseTime := readCollapseTime(db)
	today := (nowSec - crt) / 86400
	if today < 0 {
		today = 0
	}

	rawByDeck := loadRawCardCountsByDeck(db, today, nowSec+collapseTime)
	configs := loadSQLiteDeckConfigs(db)
	treeCounts := computeDeckTreeCounts(decks, configs, rawByDeck, today)

	var remainingNew, remainingRev, remainingLrn int
	target := strings.TrimSpace(selectedDeck)
	if target != "" {
		if root, ok := decksByName[target]; ok {
			tc := treeCounts[root.id]
			remainingNew = tc.subtreeNew
			remainingRev = tc.subtreeRev
			remainingLrn = tc.subtreeLrn
		}
	} else {
		for _, d := range decks {
			if parentName := deckParentName(d.name); parentName != "" {
				if _, hasParent := decksByName[parentName]; hasParent {
					continue
				}
			}
			tc := treeCounts[d.id]
			remainingNew += tc.subtreeNew
			remainingRev += tc.subtreeRev
			remainingLrn += tc.subtreeLrn
		}
	}

	remainingDue := remainingLrn + remainingRev
	totalRemaining := remainingDue + remainingNew
	if totalRemaining == 0 {
		return &ParsedCard{
			DeckName:       selectedDeck,
			RemainingDue:   0,
			RemainingNew:   0,
			TotalRemaining: 0,
			SourceMode:     "sqlite",
		}, nil
	}

	var eligibleRevIDs, eligibleNewIDs []int64
	for _, id := range matchingIDs {
		tc := treeCounts[id]
		if tc.canDrawRev {
			eligibleRevIDs = append(eligibleRevIDs, id)
		}
		if tc.canDrawNew {
			eligibleNewIDs = append(eligibleNewIDs, id)
		}
	}

	var cardID, noteID, cardDeckID int64
	var cardOrd int
	isDue := false
	found := false

	inAll, allArgs := buildInClause(matchingIDs)

	// 1. Ready learning/relearning cards (due <= nowSec for intra-day queue=1, due <= today for inter-day queue=3)
	lrnArgs := append([]interface{}{nowSec, today}, allArgs...)
	if err = db.QueryRow(
		fmt.Sprintf(
			`SELECT id, nid, did, ord FROM cards
			 WHERE ((queue = 1 AND due <= ?) OR (queue = 3 AND due <= ?)) AND did IN %s
			 ORDER BY due ASC, id ASC
			 LIMIT 1`,
			inAll,
		),
		lrnArgs...,
	).Scan(&cardID, &noteID, &cardDeckID, &cardOrd); err == nil {
		isDue = true
		found = true
	}

	// 2. Due review cards (respecting daily deck/ancestor review quotas)
	if !found && remainingRev > 0 && len(eligibleRevIDs) > 0 {
		inRev, revArgs := buildInClause(eligibleRevIDs)
		qArgs := append([]interface{}{today}, revArgs...)
		if err = db.QueryRow(
			fmt.Sprintf(
				`SELECT id, nid, did, ord FROM cards
				 WHERE queue = 2 AND due <= ? AND did IN %s
				 ORDER BY due ASC, id ASC
				 LIMIT 1`,
				inRev,
			),
			qArgs...,
		).Scan(&cardID, &noteID, &cardDeckID, &cardOrd); err == nil {
			isDue = true
			found = true
		}
	}

	// 3. New cards (respecting daily deck/ancestor new quotas)
	if !found && remainingNew > 0 && len(eligibleNewIDs) > 0 {
		inNew, newArgs := buildInClause(eligibleNewIDs)
		if err = db.QueryRow(
			fmt.Sprintf(
				`SELECT id, nid, did, ord FROM cards
				 WHERE queue = 0 AND did IN %s
				 ORDER BY due ASC, id ASC
				 LIMIT 1`,
				inNew,
			),
			newArgs...,
		).Scan(&cardID, &noteID, &cardDeckID, &cardOrd); err == nil {
			isDue = false
			found = true
		}
	}

	// 4. Learn-ahead fallback (queue=1 due within collapseTime) when no other cards are ready
	if !found {
		aheadArgs := append([]interface{}{nowSec + collapseTime}, allArgs...)
		if err = db.QueryRow(
			fmt.Sprintf(
				`SELECT id, nid, did, ord FROM cards
				 WHERE queue = 1 AND due <= ? AND did IN %s
				 ORDER BY due ASC, id ASC
				 LIMIT 1`,
				inAll,
			),
			aheadArgs...,
		).Scan(&cardID, &noteID, &cardDeckID, &cardOrd); err == nil {
			isDue = true
			found = true
		}
	}

	if !found {
		return &ParsedCard{
			DeckName:       selectedDeck,
			RemainingDue:   0,
			RemainingNew:   0,
			TotalRemaining: 0,
			SourceMode:     "sqlite",
		}, nil
	}

	var modelID int64
	var rawFlds, noteTags string
	noteCols := tableColumns(db, "notes")
	if noteCols["tags"] {
		err = db.QueryRow("SELECT mid, flds, tags FROM notes WHERE id = ?", noteID).Scan(&modelID, &rawFlds, &noteTags)
	} else {
		err = db.QueryRow("SELECT mid, flds FROM notes WHERE id = ?", noteID).Scan(&modelID, &rawFlds)
	}
	if err != nil {
		return nil, fmt.Errorf("query note %d: %w", noteID, err)
	}

	fieldNames, _ := loadSQLiteFields(db, modelID)
	rawValues := strings.Split(rawFlds, "\x1f")
	fieldsMap := make(map[string]FieldData, len(rawValues))
	for i, val := range rawValues {
		name := fmt.Sprintf("Field_%d", i+1)
		if i < len(fieldNames) && strings.TrimSpace(fieldNames[i]) != "" {
			name = fieldNames[i]
		}
		fieldsMap[name] = FieldData{
			Value: val,
			Order: i,
		}
	}

	deckName := deckNamesByID[cardDeckID]
	if deckName == "" {
		deckName = selectedDeck
	}

	var frontHTML, backHTML string
	var frontAudio, backAudio, audioFiles []string
	tmpl, modelName, hasTmpl := loadSQLiteTemplate(db, modelID, cardOrd)
	if len(frontFields) > 0 || len(backFields) > 0 {
		frontHTML, backHTML, frontAudio, backAudio, audioFiles = RenderSelectedFields(
			fieldsMap, frontFields, backFields, tmpl.CSS,
		)
	} else if hasTmpl {
		frontHTML, backHTML, frontAudio, backAudio, audioFiles = RenderCardTemplate(
			tmpl, fieldsMap, noteTags, deckName, modelName,
		)
	} else {
		frontHTML, backHTML, frontAudio, backAudio, audioFiles = FormatFieldsMap(fieldsMap, "", "")
	}

	return &ParsedCard{
		CardID:         cardID,
		DeckName:       deckName,
		FrontHTML:      frontHTML,
		BackHTML:       backHTML,
		AudioFiles:     audioFiles,
		FrontAudio:     frontAudio,
		BackAudio:      backAudio,
		IsDue:          isDue,
		RemainingDue:   remainingDue,
		RemainingNew:   remainingNew,
		TotalRemaining: totalRemaining,
		SourceMode:     "sqlite",
	}, nil
}

// SQLiteAnswerCard records an SM-2 card review directly into collection.anki2 with usn=-1
// and updates col.mod + decks.common counters so Desktop Anki syncs and deducts daily quotas.
func SQLiteAnswerCard(collectionPath string, cardID int64, ease int) error {
	return SQLiteAnswerCardWithElapsed(collectionPath, cardID, ease, 8000)
}

// SQLiteAnswerCardWithElapsed records an SM-2 card review directly into collection.anki2
// with the measured review elapsed time in milliseconds.
func SQLiteAnswerCardWithElapsed(collectionPath string, cardID int64, ease int, elapsedMs int) error {
	if cardID <= 0 {
		return errors.New("invalid cardId")
	}
	if ease < 1 || ease > 4 {
		return fmt.Errorf("ease must be between 1 and 4, got %d", ease)
	}
	if isAnkiRunningFunc() {
		return errors.New("Anki Desktop is running; review via AnkiConnect or close Anki first")
	}
	if elapsedMs <= 0 {
		elapsedMs = 8000
	} else if elapsedMs < 100 {
		elapsedMs = 100
	} else if elapsedMs > 60000 {
		elapsedMs = 60000
	}

	db, err := openSQLite(collectionPath, false)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin sqlite tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var crt int64
	if err := tx.QueryRow("SELECT crt FROM col LIMIT 1").Scan(&crt); err != nil {
		return fmt.Errorf("read col crt: %w", err)
	}

	var cardDeckID int64
	var cardType, queue, ivl, factor, reps, lapses int
	var due int64
	var dataStr string
	if err := tx.QueryRow(
		`SELECT did, type, queue, due, ivl, factor, reps, lapses, data FROM cards WHERE id = ?`,
		cardID,
	).Scan(&cardDeckID, &cardType, &queue, &due, &ivl, &factor, &reps, &lapses, &dataStr); err != nil {
		return fmt.Errorf("load card %d: %w", cardID, err)
	}

	nowSec := time.Now().Unix()
	nowMs := time.Now().UnixMilli()
	today := (nowSec - crt) / 86400
	if today < 0 {
		today = 0
	}

	oldIvl := ivl
	if factor <= 0 {
		factor = 2500
	}

	var revlogType int
	switch cardType {
	case 0:
		revlogType = 0 // learn
	case 2:
		revlogType = 1 // review
	default:
		revlogType = 2 // relearn
	}

	newType := cardType
	newQueue := queue
	newDue := due
	newIvl := ivl
	newFactor := factor
	newLapses := lapses
	newLeft := 0
	revlogIvl := 0
	revlogFactor := factor

	if cardType == 0 || cardType == 1 || cardType == 3 {
		// New / Learning / Relearning card
		switch ease {
		case 1:
			if cardType == 3 {
				newType = 3
			} else {
				newType = 1
			}
			newQueue = 1
			newDue = nowSec + 60
			newIvl = 0
			newLeft = 1
			revlogIvl = -60
			revlogFactor = 0
		case 2:
			if cardType == 3 {
				newType = 3
			} else {
				newType = 1
			}
			newQueue = 1
			newDue = nowSec + 360
			newIvl = 0
			newLeft = 1
			revlogIvl = -360
			revlogFactor = 0
		case 3:
			if cardType == 0 {
				// First Good on a brand-new card enters the learning step (10m) instead of instant graduation.
				newType = 1
				newQueue = 1
				newDue = nowSec + 600
				newIvl = 0
				newLeft = 1
				revlogIvl = -600
				revlogFactor = 0
			} else {
				newType = 2
				newQueue = 2
				newIvl = 1
				if oldIvl > 1 {
					newIvl = oldIvl
				}
				newDue = today + int64(newIvl)
				newLeft = 0
				revlogIvl = newIvl
				revlogFactor = newFactor
			}
		case 4:
			newType = 2
			newQueue = 2
			newIvl = 4
			if oldIvl > 4 {
				newIvl = oldIvl
			}
			newDue = today + int64(newIvl)
			newLeft = 0
			revlogIvl = newIvl
			revlogFactor = newFactor
		}
	} else {
		// Review card (cardType == 2)
		baseIvl := oldIvl
		if baseIvl < 1 {
			baseIvl = 1
		}
		switch ease {
		case 1:
			newLapses = lapses + 1
			newType = 3
			newQueue = 1
			newDue = nowSec + 600
			newIvl = 1
			newLeft = 1
			newFactor = factor - 200
			if newFactor < 1300 {
				newFactor = 1300
			}
			revlogIvl = -600
			revlogFactor = newFactor
		case 2:
			newType = 2
			newQueue = 2
			newIvl = int(float64(baseIvl) * 1.2)
			if newIvl <= baseIvl {
				newIvl = baseIvl + 1
			}
			newDue = today + int64(newIvl)
			newFactor = factor - 150
			if newFactor < 1300 {
				newFactor = 1300
			}
			revlogIvl = newIvl
			revlogFactor = newFactor
		case 3:
			newType = 2
			newQueue = 2
			newIvl = int(float64(baseIvl) * (float64(factor) / 1000.0))
			if newIvl <= baseIvl {
				newIvl = baseIvl + 1
			}
			newDue = today + int64(newIvl)
			revlogIvl = newIvl
			revlogFactor = newFactor
		case 4:
			newType = 2
			newQueue = 2
			newIvl = int(float64(baseIvl) * (float64(factor) / 1000.0) * 1.3)
			if newIvl < baseIvl+2 {
				newIvl = baseIvl + 2
			}
			newDue = today + int64(newIvl)
			newFactor = factor + 150
			revlogIvl = newIvl
			revlogFactor = newFactor
		}
	}

	updatedData := updateCardDataLRT(dataStr, nowSec)

	if _, err := tx.Exec(
		`UPDATE cards
		 SET type = ?, queue = ?, due = ?, ivl = ?, factor = ?, reps = reps + 1, lapses = ?, left = ?, mod = ?, usn = -1, data = ?
		 WHERE id = ?`,
		newType, newQueue, newDue, newIvl, newFactor, newLapses, newLeft, nowSec, updatedData, cardID,
	); err != nil {
		return fmt.Errorf("update card %d: %w", cardID, err)
	}

	revlogID := nowMs
	for {
		var exists int
		if checkErr := tx.QueryRow("SELECT 1 FROM revlog WHERE id = ?", revlogID).Scan(&exists); checkErr == sql.ErrNoRows {
			break
		}
		revlogID++
	}

	if _, err := tx.Exec(
		`INSERT INTO revlog (id, cid, usn, ease, ivl, lastIvl, factor, time, type)
		 VALUES (?, ?, -1, ?, ?, ?, ?, ?, ?)`,
		revlogID, cardID, ease, revlogIvl, oldIvl, revlogFactor, elapsedMs, revlogType,
	); err != nil {
		return fmt.Errorf("insert revlog for card %d: %w", cardID, err)
	}

	// Update decks.common studied counters across target deck + all \x1f ancestor decks (Schema v18).
	if err := updateDeckStudiedCountersTx(tx, cardDeckID, cardType, today, nowSec, elapsedMs); err != nil {
		return err
	}

	// Bump col.mod in epoch milliseconds so Anki rslib detects local changes (col.mod > col.ls) and syncs.
	colCols := tableColumns(tx, "col")
	if colCols["mod"] {
		if _, err := tx.Exec(
			`UPDATE col SET mod = CASE WHEN mod >= ? THEN mod + 1 ELSE ? END`,
			nowMs, nowMs,
		); err != nil {
			return fmt.Errorf("update col.mod: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit review tx: %w", err)
	}
	return nil
}

func updateDeckStudiedCountersTx(
	tx *sql.Tx,
	cardDeckID int64,
	cardType int,
	today int64,
	nowSec int64,
	elapsedMs int,
) error {
	deckCols := tableColumns(tx, "decks")
	if !deckCols["common"] || !deckCols["mtime_secs"] || !deckCols["usn"] {
		return nil
	}

	var targetRawName string
	if err := tx.QueryRow("SELECT name FROM decks WHERE id = ?", cardDeckID).Scan(&targetRawName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup deck %d: %w", cardDeckID, err)
	}

	// Collect target deck and every \x1f-delimited ancestor name.
	// Note: Query all decks without WHERE name IN (...) because Anki's decks.name uses
	// custom COLLATE unicase which is not registered in modernc.org/sqlite.
	parts := strings.Split(targetRawName, "\x1f")
	ancestorSet := make(map[string]bool, len(parts))
	for i := 1; i <= len(parts); i++ {
		ancestorSet[strings.Join(parts[:i], "\x1f")] = true
	}

	rows, err := tx.Query("SELECT id, name, common FROM decks")
	if err != nil {
		return fmt.Errorf("query ancestor decks: %w", err)
	}
	type deckCommonRow struct {
		id     int64
		common []byte
	}
	var updates []deckCommonRow
	for rows.Next() {
		var r deckCommonRow
		var rawName string
		if scanErr := rows.Scan(&r.id, &rawName, &r.common); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		if ancestorSet[rawName] {
			updates = append(updates, r)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		_ = rows.Close()
		return rowsErr
	}
	_ = rows.Close()

	for _, u := range updates {
		dc := decodeDeckCommon(u.common)
		if int64(dc.LastDayStudied) != today {
			dc.LastDayStudied = uint32(today)
			dc.NewStudied = 0
			dc.ReviewStudied = 0
			dc.LearningStudied = 0
			dc.MillisecondsStudied = 0
		}
		switch cardType {
		case 0:
			dc.NewStudied++
		case 2:
			dc.ReviewStudied++
		}
		dc.MillisecondsStudied += int32(elapsedMs)
		encoded := encodeDeckCommon(dc)
		if _, err := tx.Exec(
			`UPDATE decks SET common = ?, mtime_secs = ?, usn = -1 WHERE id = ?`,
			encoded, nowSec, u.id,
		); err != nil {
			return fmt.Errorf("update deck %d common: %w", u.id, err)
		}
	}
	return nil
}

func updateCardDataLRT(dataStr string, nowSec int64) string {
	trimmed := strings.TrimSpace(dataStr)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return dataStr
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return dataStr
	}
	payload["lrt"] = nowSec
	encoded, err := json.Marshal(payload)
	if err != nil {
		return dataStr
	}
	return string(encoded)
}
