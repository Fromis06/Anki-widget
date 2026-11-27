package anki

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const ankiConnectVersion = 6

var (
	soundTagRe = regexp.MustCompile(`\[sound:([^\]]+)\]`)
	furiganaRe = regexp.MustCompile(`([\p{Han}々ヶ〆〇]+)\[([^\[\]<>]+)\]`)
)

type ankiconnectRequest struct {
	Action  string      `json:"action"`
	Version int         `json:"version"`
	Params  interface{} `json:"params,omitempty"`
}

type ankiconnectResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error"`
}

// FieldData represents a single field entry returned by AnkiConnect's cardsInfo action.
type FieldData struct {
	Value string `json:"value"`
	Order int    `json:"order"`
}

// CardInfo mirrors the card object returned by AnkiConnect's cardsInfo action.
type CardInfo struct {
	CardID    int64                `json:"cardId"`
	NoteID    int64                `json:"note"`
	DeckName  string               `json:"deckName"`
	ModelName string               `json:"modelName"`
	Ord       int                  `json:"ord"`
	CSS       string               `json:"css"`
	Question  string               `json:"question"`
	Answer    string               `json:"answer"`
	Fields    map[string]FieldData `json:"fields"`
	Queue     int                  `json:"queue"`
	Type      int                  `json:"type"`
	Interval  int                  `json:"interval"`
}

// DeckStats mirrors the per-deck statistics returned by AnkiConnect's getDeckStats action.
type DeckStats struct {
	DeckID      int64  `json:"deck_id"`
	Name        string `json:"name"`
	NewCount    int    `json:"new_count"`
	LearnCount  int    `json:"learn_count"`
	ReviewCount int    `json:"review_count"`
	TotalInDeck int    `json:"total_in_deck"`
}

type ankiModelDefinition struct {
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

// ParsedCard contains sanitized HTML and extracted audio filenames ready for the frontend widget.
type ParsedCard struct {
	CardID         int64    `json:"cardId"`
	DeckName       string   `json:"deckName"`
	FrontHTML      string   `json:"frontHtml"`
	BackHTML       string   `json:"backHtml"`
	AudioFiles     []string `json:"audioFiles"`
	FrontAudio     []string `json:"frontAudio"`
	BackAudio      []string `json:"backAudio"`
	IsDue          bool     `json:"isDue"`
	RemainingDue   int      `json:"remainingDue"`
	RemainingNew   int      `json:"remainingNew"`
	TotalRemaining int      `json:"totalRemaining"`
	SourceMode     string   `json:"sourceMode"`
}

// Client communicates with the local AnkiConnect HTTP server.
type Client struct {
	endpoint        string
	httpClient      *http.Client
	pingClient      *http.Client
	retryPingClient *http.Client
	mu              sync.RWMutex
	mediaDir        string
	modelCache      map[string]ankiModelDefinition
}

// NewClient initializes a new AnkiConnect client with fast local dial timeouts and no proxy lookup overhead.
func NewClient(endpoint string) *Client {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		ep = "http://127.0.0.1:8765"
	}
	fastTransport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: 150 * time.Millisecond,
		}).DialContext,
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	}
	return &Client{
		endpoint: ep,
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: fastTransport,
		},
		pingClient: &http.Client{
			Timeout:   250 * time.Millisecond,
			Transport: fastTransport,
		},
		retryPingClient: &http.Client{
			Timeout:   1500 * time.Millisecond,
			Transport: fastTransport,
		},
		modelCache: make(map[string]ankiModelDefinition),
	}
}

// Endpoint returns the currently configured AnkiConnect endpoint URL.
func (c *Client) Endpoint() string {
	return c.getEndpoint()
}

// IsLocalLoopbackEndpoint reports whether the configured endpoint targets the local machine.
func IsLocalLoopbackEndpoint(endpoint string) bool {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	return host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// SetEndpoint updates the AnkiConnect URL and resets cached media directory and model templates.
func (c *Client) SetEndpoint(endpoint string) {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		ep = "http://127.0.0.1:8765"
	}
	c.mu.Lock()
	c.endpoint = ep
	c.mediaDir = ""
	c.modelCache = make(map[string]ankiModelDefinition)
	c.mu.Unlock()
}

func (c *Client) getEndpoint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.endpoint
}

func (c *Client) call(action string, params interface{}, out interface{}) error {
	return c.callWithClient(c.httpClient, action, params, out)
}

func (c *Client) callWithClient(hc *http.Client, action string, params interface{}, out interface{}) error {
	payload := ankiconnectRequest{
		Action:  action,
		Version: ankiConnectVersion,
		Params:  params,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal ankiconnect request %s: %w", action, err)
	}

	req, err := http.NewRequest(http.MethodPost, c.getEndpoint(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build ankiconnect request %s: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("ankiconnect unavailable (%s): %w", action, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read ankiconnect response %s: %w", action, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ankiconnect HTTP %d for %s: %s", resp.StatusCode, action, strings.TrimSpace(string(respBytes)))
	}

	var envelope ankiconnectResponse
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return fmt.Errorf("decode ankiconnect response %s: %w", action, err)
	}
	if envelope.Error != nil && *envelope.Error != "" {
		return fmt.Errorf("ankiconnect error (%s): %s", action, *envelope.Error)
	}

	if out != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, out); err != nil {
			return fmt.Errorf("decode ankiconnect result %s: %w", action, err)
		}
	}
	return nil
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// Ping checks whether AnkiConnect is currently reachable within 250ms.
func (c *Client) Ping() error {
	var ver int
	return c.callWithClient(c.pingClient, "version", nil, &ver)
}

// PingStatus distinguishes a reachable AnkiConnect server, a busy/timeout response, and a closed socket.
func (c *Client) PingStatus() (available bool, timedOut bool, err error) {
	var ver int
	err = c.callWithClient(c.pingClient, "version", nil, &ver)
	if err == nil {
		return true, false, nil
	}
	if isTimeoutErr(err) {
		// Retry once with a longer timeout in case Anki Desktop's Qt event loop was momentarily busy.
		if retryErr := c.callWithClient(c.retryPingClient, "version", nil, &ver); retryErr == nil {
			return true, false, nil
		} else if isTimeoutErr(retryErr) {
			return false, true, retryErr
		} else {
			return false, false, retryErr
		}
	}
	return false, false, err
}

// IsAvailable reports whether Anki Desktop + AnkiConnect is currently running and reachable.
func (c *Client) IsAvailable() bool {
	ok, _, _ := c.PingStatus()
	return ok
}

// Sync triggers AnkiWeb synchronization via AnkiConnect's sync action.
func (c *Client) Sync() error {
	return c.call("sync", nil, nil)
}

// ExtractSoundTags extracts [sound:filename] references, strips CSS/scripts/general HTML tags
// and <hr id=answer>, and converts bracket furigana like 漢字[かんじ] into <ruby> markup.
func ExtractSoundTags(raw string) (string, []string) {
	if raw == "" {
		return "", nil
	}

	cleaned := htmlCommentRe.ReplaceAllString(raw, "")
	cleaned = hrAnswerTagRe.ReplaceAllString(cleaned, "")
	return sanitizeAnkiHTMLAndExtractSounds(cleaned, "")
}

// DeckNames fetches all deck names from Anki. When onlyDue is true, it filters decks
// that have due or new non-suspended cards (falling back to all decks if none match).
func (c *Client) DeckNames(onlyDue bool) ([]string, error) {
	var decks []string
	if err := c.call("deckNames", nil, &decks); err != nil {
		return nil, err
	}
	sort.Slice(decks, func(i, j int) bool {
		return strings.ToLower(decks[i]) < strings.ToLower(decks[j])
	})

	if !onlyDue || len(decks) == 0 {
		return decks, nil
	}

	dueDecks := make([]string, 0, len(decks))
	for _, deck := range decks {
		query := fmt.Sprintf(`deck:"%s" (is:due OR is:new) -is:suspended`, escapeQueryValue(deck))
		ids, err := c.FindCards(query)
		if err == nil && len(ids) > 0 {
			dueDecks = append(dueDecks, deck)
		}
	}

	if len(dueDecks) == 0 {
		return decks, nil
	}
	return dueDecks, nil
}

// GetDeckStats retrieves Anki scheduler-capped counts (new_count, learn_count, review_count)
// for the specified deck names via AnkiConnect's getDeckStats action.
func (c *Client) GetDeckStats(decks []string) (map[string]DeckStats, error) {
	if len(decks) == 0 {
		return nil, nil
	}
	var stats map[string]DeckStats
	if err := c.call("getDeckStats", map[string][]string{"decks": decks}, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// FindCards runs an Anki search query and returns matching card IDs.
func (c *Client) FindCards(query string) ([]int64, error) {
	var ids []int64
	err := c.call("findCards", map[string]string{"query": query}, &ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// CardsInfo retrieves detailed card data for the supplied card IDs.
func (c *Client) CardsInfo(cardIDs []int64) ([]CardInfo, error) {
	if len(cardIDs) == 0 {
		return nil, nil
	}
	var cards []CardInfo
	err := c.call("cardsInfo", map[string][]int64{"cards": cardIDs}, &cards)
	if err != nil {
		return nil, err
	}
	return cards, nil
}

func (c *Client) getCardTemplate(modelName string, ord int, fallbackCSS string) (CardTemplate, bool) {
	name := strings.TrimSpace(modelName)
	if name == "" {
		return CardTemplate{}, false
	}

	c.mu.RLock()
	cached, ok := c.modelCache[name]
	c.mu.RUnlock()

	if !ok {
		var models []ankiModelDefinition
		if err := c.call("findModelsByName", map[string][]string{"modelNames": {name}}, &models); err != nil || len(models) == 0 {
			return CardTemplate{}, false
		}
		cached = models[0]
		c.mu.Lock()
		if c.modelCache == nil {
			c.modelCache = make(map[string]ankiModelDefinition)
		}
		c.modelCache[name] = cached
		c.mu.Unlock()
	}

	isCloze := cached.Type == 1
	targetOrd := ord
	if isCloze {
		targetOrd = 0
	}
	css := cached.CSS
	if strings.TrimSpace(css) == "" {
		css = fallbackCSS
	}

	for _, t := range cached.Tmpls {
		if t.Ord == targetOrd || (len(cached.Tmpls) == 1 && targetOrd == 0) {
			return CardTemplate{
				Ord:     ord,
				Name:    t.Name,
				Qfmt:    t.Qfmt,
				Afmt:    t.Afmt,
				CSS:     css,
				IsCloze: isCloze,
			}, true
		}
	}
	return CardTemplate{}, false
}

func (c *Client) fetchSchedulerCounts(selectedDeck string) (dueCount int, newCount int, ok bool) {
	target := strings.TrimSpace(selectedDeck)
	var queryDecks []string
	if target != "" {
		queryDecks = []string{target}
	} else {
		allDecks, err := c.DeckNames(false)
		if err != nil || len(allDecks) == 0 {
			return 0, 0, false
		}
		// Top-level deck nodes in Anki's deck_due_tree already aggregate all child subdecks.
		for _, d := range allDecks {
			if !strings.Contains(d, "::") {
				queryDecks = append(queryDecks, d)
			}
		}
		if len(queryDecks) == 0 {
			queryDecks = allDecks
		}
	}

	stats, err := c.GetDeckStats(queryDecks)
	if err != nil || len(stats) == 0 {
		return 0, 0, false
	}
	for _, st := range stats {
		dueCount += st.LearnCount + st.ReviewCount
		newCount += st.NewCount
	}
	return dueCount, newCount, true
}

// GetDeckFields samples cards in the selected deck via AnkiConnect and returns available fields + template defaults.
func (c *Client) GetDeckFields(selectedDeck string) (*DeckFieldsInfo, error) {
	scope := ""
	if strings.TrimSpace(selectedDeck) != "" {
		scope = fmt.Sprintf(`deck:"%s" `, escapeQueryValue(selectedDeck))
	}
	ids, err := c.FindCards(scope + "-is:suspended")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &DeckFieldsInfo{
			Fields:       []string{},
			DefaultFront: []string{},
			DefaultBack:  []string{},
		}, nil
	}
	if len(ids) > 25 {
		ids = ids[:25]
	}
	cards, err := c.CardsInfo(ids)
	if err != nil {
		return nil, err
	}

	seenFields := make(map[string]bool)
	seenFront := make(map[string]bool)
	seenBack := make(map[string]bool)
	info := &DeckFieldsInfo{
		Fields:       []string{},
		DefaultFront: []string{},
		DefaultBack:  []string{},
	}

	for _, card := range cards {
		type orderedName struct {
			ord  int
			name string
		}
		var ordered []orderedName
		canonicalByLower := make(map[string]string, len(card.Fields))
		for name, f := range card.Fields {
			ordered = append(ordered, orderedName{ord: f.Order, name: name})
			canonicalByLower[strings.ToLower(strings.TrimSpace(name))] = name
		}
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].ord == ordered[j].ord {
				return ordered[i].name < ordered[j].name
			}
			return ordered[i].ord < ordered[j].ord
		})
		for _, o := range ordered {
			if !seenFields[o.name] {
				seenFields[o.name] = true
				info.Fields = append(info.Fields, o.name)
			}
		}
		if tmpl, hasTmpl := c.getCardTemplate(card.ModelName, card.Ord, card.CSS); hasTmpl {
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

// FetchNextCard prioritizes due cards over new cards for the target deck (or all decks when empty),
// parses card templates & [sound:...] tags, and returns a ParsedCard with scheduler-capped remaining counts.
func (c *Client) FetchNextCard(selectedDeck string) (*ParsedCard, error) {
	return c.FetchNextCardWithFields(selectedDeck, nil, nil)
}

// IsQueueMismatchError reports whether an answer submission failed because the card is no longer at the top of Anki's queue.
func IsQueueMismatchError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not at top of queue") ||
		strings.Contains(msg, "not on top") ||
		strings.Contains(msg, "rejected answer")
}

func (c *Client) guiCurrentCard() (*CardInfo, error) {
	var card CardInfo
	if err := c.call("guiCurrentCard", nil, &card); err != nil {
		return nil, err
	}
	if card.CardID <= 0 {
		return nil, nil
	}
	return &card, nil
}

func deckMatchesScope(cardDeck, selectedDeck string) bool {
	target := strings.TrimSpace(selectedDeck)
	if target == "" {
		return true
	}
	return cardDeck == target || strings.HasPrefix(cardDeck, target+"::")
}

func normalizeStudyMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "due":
		return "due"
	case "new":
		return "new"
	default:
		return "both"
	}
}

func containsCardID(ids []int64, target int64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// FetchNextCardWithFields fetches the next due/new card and renders user-selected Front/Back fields if provided.
func (c *Client) FetchNextCardWithFields(selectedDeck string, frontFields, backFields []string) (*ParsedCard, error) {
	return c.FetchNextCardWithOptions(selectedDeck, frontFields, backFields, "both")
}

// FetchNextCardWithOptions fetches the next card matching studyMode ("due", "new", or "both")
// and renders user-selected Front/Back fields if provided.
func (c *Client) FetchNextCardWithOptions(selectedDeck string, frontFields, backFields []string, studyMode string) (*ParsedCard, error) {
	mode := normalizeStudyMode(studyMode)
	scope := ""
	if strings.TrimSpace(selectedDeck) != "" {
		scope = fmt.Sprintf(`deck:"%s" `, escapeQueryValue(selectedDeck))
	}

	var (
		learnIDs   []int64
		dueIDs     []int64
		newIDs     []int64
		dueErr     error
		newErr     error
		guiCard    *CardInfo
		cappedDue  int
		cappedNew  int
		hasStats   bool
		fetchGroup sync.WaitGroup
	)
	if mode != "new" {
		fetchGroup.Add(2)
		go func() {
			defer fetchGroup.Done()
			learnIDs, _ = c.FindCards(scope + "is:learn is:due -is:suspended")
		}()
		go func() {
			defer fetchGroup.Done()
			dueIDs, dueErr = c.FindCards(scope + "is:due -is:suspended")
		}()
	}
	if mode != "due" {
		fetchGroup.Add(1)
		go func() {
			defer fetchGroup.Done()
			newIDs, newErr = c.FindCards(scope + "is:new -is:suspended")
		}()
	}
	fetchGroup.Add(2)
	go func() {
		defer fetchGroup.Done()
		cappedDue, cappedNew, hasStats = c.fetchSchedulerCounts(selectedDeck)
	}()
	go func() {
		defer fetchGroup.Done()
		guiCard, _ = c.guiCurrentCard()
	}()
	fetchGroup.Wait()

	if dueErr != nil {
		return nil, dueErr
	}
	if newErr != nil {
		return nil, newErr
	}

	remainingDue := 0
	remainingNew := 0
	if mode != "new" {
		remainingDue = len(dueIDs)
		if hasStats {
			remainingDue = cappedDue
		}
	}
	if mode != "due" {
		remainingNew = len(newIDs)
		if hasStats {
			remainingNew = cappedNew
		}
	}

	totalRemaining := remainingDue + remainingNew
	hasEligible := false
	switch mode {
	case "due":
		hasEligible = len(learnIDs) > 0 || len(dueIDs) > 0
	case "new":
		hasEligible = len(newIDs) > 0
	default:
		hasEligible = len(learnIDs) > 0 || len(dueIDs) > 0 || len(newIDs) > 0
	}
	if totalRemaining == 0 || !hasEligible {
		return &ParsedCard{
			DeckName:       selectedDeck,
			RemainingDue:   0,
			RemainingNew:   0,
			TotalRemaining: 0,
			SourceMode:     "ankiconnect",
		}, nil
	}

	guiMatchesScope := guiCard != nil && guiCard.CardID > 0 && deckMatchesScope(guiCard.DeckName, selectedDeck)
	guiInDue := guiMatchesScope && (containsCardID(learnIDs, guiCard.CardID) || containsCardID(dueIDs, guiCard.CardID))
	guiInNew := guiMatchesScope && containsCardID(newIDs, guiCard.CardID)

	var chosenID int64
	isDue := false
	switch mode {
	case "due":
		switch {
		case guiInDue:
			chosenID = guiCard.CardID
			isDue = true
		case remainingDue > 0 && len(learnIDs) > 0:
			chosenID = learnIDs[0]
			isDue = true
		case remainingDue > 0 && len(dueIDs) > 0:
			chosenID = dueIDs[0]
			isDue = true
		case len(learnIDs) > 0:
			chosenID = learnIDs[0]
			isDue = true
		default:
			chosenID = dueIDs[0]
			isDue = true
		}
	case "new":
		if guiInNew {
			chosenID = guiCard.CardID
		} else {
			chosenID = newIDs[0]
		}
		isDue = false
	default:
		switch {
		case guiInDue:
			chosenID = guiCard.CardID
			isDue = true
		case guiInNew:
			chosenID = guiCard.CardID
			isDue = false
		case guiMatchesScope:
			chosenID = guiCard.CardID
			isDue = true
		case remainingDue > 0 && len(learnIDs) > 0:
			chosenID = learnIDs[0]
			isDue = true
		case remainingDue > 0 && len(dueIDs) > 0:
			chosenID = dueIDs[0]
			isDue = true
		case remainingNew > 0 && len(newIDs) > 0:
			chosenID = newIDs[0]
			isDue = false
		case len(learnIDs) > 0:
			chosenID = learnIDs[0]
			isDue = true
		case len(dueIDs) > 0:
			chosenID = dueIDs[0]
			isDue = true
		default:
			chosenID = newIDs[0]
			isDue = false
		}
	}

	cards, err := c.CardsInfo([]int64{chosenID})
	if err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("cardsInfo returned empty result for card %d", chosenID)
	}

	info := cards[0]
	var frontHTML, backHTML string
	var frontAudio, backAudio, audioFiles []string

	if len(frontFields) > 0 || len(backFields) > 0 {
		frontHTML, backHTML, frontAudio, backAudio, audioFiles = RenderSelectedFields(
			info.Fields, frontFields, backFields, info.CSS,
		)
	} else if tmpl, hasTmpl := c.getCardTemplate(info.ModelName, info.Ord, info.CSS); hasTmpl {
		frontHTML, backHTML, frontAudio, backAudio, audioFiles = RenderCardTemplate(
			tmpl, info.Fields, "", info.DeckName, info.ModelName,
		)
	} else if strings.TrimSpace(info.Question) != "" || strings.TrimSpace(info.Answer) != "" {
		frontHTML, backHTML, frontAudio, backAudio, audioFiles = SplitAnkiConnectQuestionAnswer(
			info.Question, info.Answer, info.CSS,
		)
		if len(audioFiles) == 0 && len(info.Fields) > 0 {
			_, _, fAud, bAud, allAud := FormatFieldsMap(info.Fields, info.Question, info.Answer)
			frontAudio = fAud
			backAudio = bAud
			audioFiles = allAud
		}
	} else {
		frontHTML, backHTML, frontAudio, backAudio, audioFiles = FormatFieldsMap(info.Fields, info.Question, info.Answer)
	}

	return &ParsedCard{
		CardID:         info.CardID,
		DeckName:       info.DeckName,
		FrontHTML:      frontHTML,
		BackHTML:       backHTML,
		AudioFiles:     audioFiles,
		FrontAudio:     frontAudio,
		BackAudio:      backAudio,
		IsDue:          isDue,
		RemainingDue:   remainingDue,
		RemainingNew:   remainingNew,
		TotalRemaining: totalRemaining,
		SourceMode:     "ankiconnect",
	}, nil
}

// AnswerCard submits a review ease rating (1=Again, 2=Hard, 3=Good, 4=Easy) via AnkiConnect's answerCards API.
func (c *Client) AnswerCard(cardID int64, ease int) error {
	if cardID <= 0 {
		return errors.New("invalid cardId")
	}
	if ease < 1 || ease > 4 {
		return fmt.Errorf("ease must be between 1 and 4, got %d", ease)
	}

	params := map[string]interface{}{
		"answers": []map[string]interface{}{
			{
				"cardId": cardID,
				"ease":   ease,
			},
		},
	}

	var results []bool
	if err := c.call("answerCards", params, &results); err != nil {
		return err
	}
	if len(results) > 0 && !results[0] {
		return fmt.Errorf("anki rejected answer for card %d", cardID)
	}
	return nil
}

// GetMediaDirPath fetches and caches Anki's collection.media directory path.
func (c *Client) GetMediaDirPath() (string, error) {
	c.mu.RLock()
	cached := c.mediaDir
	c.mu.RUnlock()
	if cached != "" {
		return cached, nil
	}

	var dir string
	if err := c.call("getMediaDirPath", nil, &dir); err != nil {
		return "", err
	}
	dir = strings.TrimSpace(dir)
	if dir != "" {
		c.mu.Lock()
		c.mediaDir = dir
		c.mu.Unlock()
	}
	return dir, nil
}

// RetrieveMediaFile fetches a media file's bytes via AnkiConnect's retrieveMediaFile RPC.
func (c *Client) RetrieveMediaFile(filename string) ([]byte, error) {
	var b64 string
	err := c.call("retrieveMediaFile", map[string]string{"filename": filename}, &b64)
	if err != nil {
		return nil, err
	}
	if b64 == "" {
		return nil, os.ErrNotExist
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode media base64 %s: %w", filename, err)
	}
	return data, nil
}

// ResolveMediaFile resolves /media/<filename> (or bare media filename) to a local file path in collection.media
// or falls back to fetching the bytes via AnkiConnect's retrieveMediaFile API.
func (c *Client) ResolveMediaFile(rawSrc string, getConfiguredMediaDir func() string) (diskPath string, data []byte, filename string, err error) {
	rawName := strings.TrimPrefix(strings.TrimSpace(rawSrc), "/media/")
	decoded, unescErr := url.PathUnescape(rawName)
	if unescErr != nil {
		return "", nil, "", errors.New("invalid media filename")
	}
	filename = filepath.Base(strings.TrimSpace(decoded))
	if filename == "" || filename == "." || filename == ".." || strings.ContainsAny(filename, `/\`) {
		return "", nil, "", errors.New("invalid media filename")
	}

	mediaDir := ""
	if getConfiguredMediaDir != nil {
		mediaDir = strings.TrimSpace(getConfiguredMediaDir())
	}
	if mediaDir == "" && c.IsAvailable() {
		mediaDir, _ = c.GetMediaDirPath()
	}

	if mediaDir != "" {
		fullPath := filepath.Join(mediaDir, filename)
		if info, statErr := os.Stat(fullPath); statErr == nil && !info.IsDir() {
			return fullPath, nil, filename, nil
		}
	}

	if !c.IsAvailable() {
		return "", nil, filename, os.ErrNotExist
	}
	data, rpcErr := c.RetrieveMediaFile(filename)
	if rpcErr != nil {
		return "", nil, filename, rpcErr
	}
	return "", data, filename, nil
}

// MediaHandler returns an http.Handler serving /media/<filename> directly from collection.media
// (or a custom configured path), falling back to AnkiConnect's retrieveMediaFile API.
func (c *Client) MediaHandler(getConfiguredMediaDir func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/media/") {
			http.NotFound(w, r)
			return
		}

		fullPath, data, filename, err := c.ResolveMediaFile(r.URL.Path, getConfiguredMediaDir)
		if err != nil {
			if err.Error() == "invalid media filename" {
				http.Error(w, "invalid media filename", http.StatusBadRequest)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		if fullPath != "" {
			http.ServeFile(w, r, fullPath)
			return
		}

		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}

type orderedFieldEntry struct {
	order   int
	name    string
	key     string
	cleaned string
	audio   []string
}

var (
	frontFieldPriority = []string{
		"front", "expression", "từ vựng", "tu vung", "tuvung", "ngữ pháp", "word", "kanji", "term", "header",
	}
	readingFieldKeys = map[string]bool{
		"furigana": true, "kanji_furigana": true, "phienam": true, "cach doc": true, "cachdoc": true,
		"kana": true, "hanviet": true, "hán tự": true, "hantu": true, "am han": true,
		"on": true, "kun": true, "ja_on": true, "ja_kun": true, "cấu trúc": true,
	}
	meaningFieldKeys = map[string]bool{
		"nghia": true, "nghĩa": true, "ý nghĩa": true, "meaning": true,
		"vietnamese": true, "english": true, "definition": true, "cách dùng": true, "back extra": true,
	}
	exampleFieldPriority = []string{
		"ví dụ", "vd", "example", "examples", "sentence", "ex1_jap", "ex1_ja_furigana", "ex1_ja", "readingexample",
	}
	exampleTransPriority = []string{
		"sentence meaning", "ex1_vi", "ex1_en",
	}
	ignoredMetaKeys = map[string]bool{
		"id": true, "stt": true, "bài": true, "audio": true, "source": true,
		"originaljson": true, "strokes": true, "occlusion": true, "pos": true,
	}
)

// FormatFieldsMap converts Anki card fields into sanitized Front/Back HTML and extracted audio filenames
// as a graceful fallback when note type template metadata is unavailable.
func FormatFieldsMap(fields map[string]FieldData, fallbackQ, fallbackA string) (string, string, []string, []string, []string) {
	if len(fields) == 0 {
		return SplitAnkiConnectQuestionAnswer(fallbackQ, fallbackA, "")
	}

	ordered := make([]orderedFieldEntry, 0, len(fields))
	byKey := make(map[string]orderedFieldEntry, len(fields))
	var dedicatedAudio []string
	var allAudio []string

	for name, f := range fields {
		cleaned, audio := ExtractSoundTags(f.Value)
		key := strings.ToLower(strings.TrimSpace(name))
		entry := orderedFieldEntry{
			order:   f.Order,
			name:    name,
			key:     key,
			cleaned: cleaned,
			audio:   audio,
		}
		ordered = append(ordered, entry)
		byKey[key] = entry
		if key == "audio" && len(audio) > 0 {
			dedicatedAudio = mergeUniqueStrings(dedicatedAudio, audio)
		}
	}

	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].order == ordered[j].order {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].order < ordered[j].order
	})

	for _, e := range ordered {
		if len(e.audio) > 0 {
			allAudio = mergeUniqueStrings(allAudio, e.audio)
		}
	}

	var frontKey, frontHTML string
	var frontAudio []string

	for _, candidate := range frontFieldPriority {
		if e, ok := byKey[candidate]; ok && e.cleaned != "" {
			frontKey = e.key
			frontHTML = e.cleaned
			frontAudio = mergeUniqueStrings(frontAudio, e.audio)
			break
		}
	}

	if frontHTML == "" {
		if e, ok := byKey["kana"]; ok && e.cleaned != "" {
			frontKey = e.key
			frontHTML = e.cleaned
			frontAudio = mergeUniqueStrings(frontAudio, e.audio)
		}
	}

	if frontHTML == "" {
		for _, e := range ordered {
			if ignoredMetaKeys[e.key] || e.cleaned == "" {
				continue
			}
			frontKey = e.key
			frontHTML = e.cleaned
			frontAudio = mergeUniqueStrings(frontAudio, e.audio)
			break
		}
	}

	if len(frontAudio) == 0 && len(dedicatedAudio) > 0 {
		frontAudio = append([]string(nil), dedicatedAudio...)
	}

	var backHTML string
	var backAudio []string

	// Case 1: Standard Back / Mặt sau field
	for _, bKey := range []string{"back", "mặt sau"} {
		if e, ok := byKey[bKey]; ok && e.cleaned != "" {
			backHTML = e.cleaned
			backAudio = mergeUniqueStrings(backAudio, e.audio)
			break
		}
	}

	// Case 2: Multi-field structured Japanese card
	if backHTML == "" {
		var readingParts []string
		var meaningParts []string
		var usedKeys = map[string]bool{frontKey: true}

		for _, e := range ordered {
			if e.key == frontKey || e.cleaned == "" || ignoredMetaKeys[e.key] {
				continue
			}
			if readingFieldKeys[e.key] {
				readingParts = append(readingParts, e.cleaned)
				usedKeys[e.key] = true
				backAudio = mergeUniqueStrings(backAudio, e.audio)
			} else if meaningFieldKeys[e.key] {
				meaningParts = append(meaningParts, e.cleaned)
				usedKeys[e.key] = true
				backAudio = mergeUniqueStrings(backAudio, e.audio)
			}
		}

		var exampleHTML string
		for _, exKey := range exampleFieldPriority {
			if e, ok := byKey[exKey]; ok && !usedKeys[e.key] && e.cleaned != "" {
				exampleHTML = e.cleaned
				usedKeys[e.key] = true
				backAudio = mergeUniqueStrings(backAudio, e.audio)
				break
			}
		}
		var exTransHTML string
		for _, trKey := range exampleTransPriority {
			if e, ok := byKey[trKey]; ok && !usedKeys[e.key] && e.cleaned != "" {
				exTransHTML = e.cleaned
				usedKeys[e.key] = true
				break
			}
		}

		var sections []string
		if len(readingParts) > 0 {
			sections = append(sections, strings.Join(readingParts, " · "))
		}
		if len(meaningParts) > 0 {
			sections = append(sections, strings.Join(meaningParts, "<br/>"))
		}
		if exampleHTML != "" {
			exBlock := exampleHTML
			if exTransHTML != "" {
				exBlock += "<br/>" + exTransHTML
			}
			sections = append(sections, exBlock)
		}

		if len(sections) > 0 {
			backHTML = strings.Join(sections, "<br/>")
		} else {
			// Fallback: combine up to 3 remaining non-metadata fields
			var extra []string
			for _, e := range ordered {
				if usedKeys[e.key] || ignoredMetaKeys[e.key] || e.cleaned == "" {
					continue
				}
				extra = append(extra, e.cleaned)
				backAudio = mergeUniqueStrings(backAudio, e.audio)
				if len(extra) >= 3 {
					break
				}
			}
			backHTML = strings.Join(extra, "<br/>")
		}
	}

	if frontHTML == "" && fallbackQ != "" {
		var qAudio []string
		frontHTML, qAudio = ExtractSoundTags(fallbackQ)
		frontAudio = mergeUniqueStrings(frontAudio, qAudio)
		allAudio = mergeUniqueStrings(allAudio, qAudio)
	}
	if backHTML == "" && fallbackA != "" {
		var aAudio []string
		backHTML, aAudio = ExtractSoundTags(fallbackA)
		backAudio = mergeUniqueStrings(backAudio, aAudio)
		allAudio = mergeUniqueStrings(allAudio, aAudio)
	}

	return frontHTML, backHTML, frontAudio, backAudio, allAudio
}

func mergeUniqueStrings(slices ...[]string) []string {
	seen := make(map[string]struct{})
	var merged []string
	for _, s := range slices {
		for _, item := range s {
			if _, ok := seen[item]; !ok {
				seen[item] = struct{}{}
				merged = append(merged, item)
			}
		}
	}
	return merged
}

func escapeQueryValue(v string) string {
	return strings.ReplaceAll(v, `"`, `\"`)
}
